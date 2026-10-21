package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kube-haggis/kube-haggis/internal/kube"
)

// nodeActionLoader describes the native Node operation seam used by the HTTP handler.
type nodeActionLoader func(context.Context, string, string, kube.NodeActionRequest) (kube.NodeActionResult, error)

// nodeActionReviewBinding binds a normalized cluster-scoped Node operation to its exact action parameters.
func nodeActionReviewBinding(contextName, name string, request kube.NodeActionRequest) reviewBinding {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ExpectedResourceVersion = strings.TrimSpace(request.ExpectedResourceVersion)
	request.TaintKey = strings.TrimSpace(request.TaintKey)
	request.TaintValue = strings.TrimSpace(request.TaintValue)
	request.TaintEffect = strings.TrimSpace(request.TaintEffect)
	request.DryRun = false
	scope := request.Action
	if request.Action == "drain" {
		scope = strings.TrimSpace(name)
	}
	return reviewBinding{
		Context:                 strings.TrimSpace(contextName),
		Version:                 "v1",
		Resource:                "nodes",
		Name:                    strings.TrimSpace(name),
		ExpectedResourceVersion: request.ExpectedResourceVersion,
		Action:                  "node-action",
		ParametersHash:          hashReviewParameters(request),
		ConfirmationScope:       scope,
	}
}

// handleNodeAction runs one explicitly selected Node operation after the write-mode guard.
func (s *Server) handleNodeAction(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required for Node operations"})
		return
	}
	body, err := readBody(request)
	if err != nil {
		writeError(writer, err)
		return
	}
	var payload kube.NodeActionRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, fmt.Errorf("decode Node action request: %w", err))
		return
	}
	query := request.URL.Query()
	contextName := s.mutationReviewContext(request)
	binding := nodeActionReviewBinding(contextName, query.Get("name"), payload)
	if !payload.DryRun {
		if err := s.consumeMutationReview(request, binding); err != nil {
			s.recordMutation(request, nodeAuditOperation(payload.Action), "", "v1", "nodes", "", query.Get("name"), false, false, "mutation review was rejected")
			writeError(writer, err)
			return
		}
	}
	loadAction := s.nodeAction
	if loadAction == nil {
		if s.kube == nil {
			writeError(writer, fmt.Errorf("Kubernetes service is unavailable"))
			return
		}
		loadAction = s.kube.NodeAction
	}
	result, err := loadAction(request.Context(), query.Get("context"), query.Get("name"), payload)
	operation := nodeAuditOperation(payload.Action)
	if err != nil {
		s.recordMutation(request, operation, "", "v1", "nodes", "", query.Get("name"), payload.DryRun, false, nodeAuditDetail(err))
		writeError(writer, err)
		return
	}
	if payload.DryRun && result.Status == "dry-run passed" && !s.writeMutationReviewHeader(writer, binding) {
		return
	}
	if result.Status == "dry-run blocked" {
		s.recordMutation(request, operation, "", "v1", "nodes", "", query.Get("name"), true, false, "node drain dry-run was blocked by Pod limitations")
	} else {
		detail := "node action applied"
		if payload.DryRun {
			detail = "node action dry-run passed"
		}
		s.recordMutation(request, operation, "", "v1", "nodes", "", query.Get("name"), payload.DryRun, true, detail)
	}
	writeJSON(writer, http.StatusOK, result)
}

// nodeAuditOperation returns a bounded audit operation without persisting taint or drain fields.
func nodeAuditOperation(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "cordon", "uncordon", "drain", "add-taint", "remove-taint":
		return "node-" + strings.ToLower(strings.TrimSpace(action))
	default:
		return "node-action"
	}
}

// nodeAuditDetail categorizes an error without persisting request fields or Pod data.
func nodeAuditDetail(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "rbac") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		return "node action denied by Kubernetes RBAC"
	case strings.Contains(message, "resource version") || strings.Contains(message, "conflict"):
		return "node action conflicted with a newer resource version"
	case strings.Contains(message, "not supported"):
		return "node action is unsupported by the Kubernetes API"
	case strings.Contains(message, "drain blocked") || strings.Contains(message, "eviction policy") || strings.Contains(message, "poddisruptionbudget"):
		return "node drain was blocked by Pod safety limitations"
	default:
		return "node action failed"
	}
}
