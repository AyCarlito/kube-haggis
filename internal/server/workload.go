package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/AyCarlito/kube-haggis/internal/kube"
)

// workloadActionLoader describes the native workload operation seam used by the HTTP handler.
type workloadActionLoader func(context.Context, string, string, string, string, string, string, kube.WorkloadActionRequest) (kube.WorkloadActionResult, error)

// workloadActionReviewBinding binds a normalized targeted workload request to its exact action parameters.
func workloadActionReviewBinding(contextName, group, version, resource, namespace, name string, request kube.WorkloadActionRequest) reviewBinding {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ExpectedResourceVersion = strings.TrimSpace(request.ExpectedResourceVersion)
	request.Container = strings.TrimSpace(request.Container)
	request.Image = strings.TrimSpace(request.Image)
	request.Schedule = strings.TrimSpace(request.Schedule)
	request.DryRun = false
	return reviewBinding{
		Context:                 strings.TrimSpace(contextName),
		Group:                   strings.TrimSpace(group),
		Version:                 strings.TrimSpace(version),
		Resource:                strings.TrimSpace(resource),
		Namespace:               strings.TrimSpace(namespace),
		Name:                    strings.TrimSpace(name),
		Namespaced:              true,
		ExpectedResourceVersion: request.ExpectedResourceVersion,
		Action:                  "workload-action",
		ParametersHash:          hashReviewParameters(request),
		ConfirmationScope:       request.Action,
	}
}

// handleWorkloadAction runs one explicitly selected workload operation after the write-mode guard.
func (s *Server) handleWorkloadAction(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required for workload actions"})
		return
	}
	body, err := readBody(request)
	if err != nil {
		writeError(writer, err)
		return
	}
	var payload kube.WorkloadActionRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, fmt.Errorf("decode workload action request: %w", err))
		return
	}
	query := request.URL.Query()
	contextName := s.mutationReviewContext(request)
	binding := workloadActionReviewBinding(contextName, query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), query.Get("name"), payload)
	if !payload.DryRun {
		if err := s.consumeMutationReview(request, binding); err != nil {
			s.recordMutation(request, workloadAuditOperation(payload.Action), query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), query.Get("name"), false, false, "mutation review was rejected")
			writeError(writer, err)
			return
		}
	}
	loadAction := s.workloadAction
	if loadAction == nil {
		if s.kube == nil {
			writeError(writer, fmt.Errorf("Kubernetes service is unavailable"))
			return
		}
		loadAction = s.kube.WorkloadAction
	}
	result, err := loadAction(request.Context(), query.Get("context"), query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), query.Get("name"), payload)
	operation := workloadAuditOperation(payload.Action)
	if err != nil {
		s.recordMutation(request, operation, query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), query.Get("name"), payload.DryRun, false, workloadAuditDetail(err))
		writeError(writer, err)
		return
	}
	if payload.DryRun && result.Status == "dry-run passed" && !s.writeMutationReviewHeader(writer, binding) {
		return
	}
	detail := "workload action applied"
	if payload.DryRun {
		detail = "workload action dry-run passed"
	}
	s.recordMutation(request, operation, query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), query.Get("name"), payload.DryRun, true, detail)
	writeJSON(writer, http.StatusOK, result)
}

// workloadAuditOperation returns a bounded audit operation name without persisting request fields.
func workloadAuditOperation(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return "workload-action"
	}
	for _, character := range action {
		if (character < 'a' || character > 'z') && character != '-' {
			return "workload-action"
		}
	}
	return "workload-" + action
}

// workloadAuditDetail categorizes an error without persisting user-supplied action fields or bodies.
func workloadAuditDetail(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "rbac") || strings.Contains(message, "forbidden"):
		return "workload action denied by Kubernetes RBAC"
	case strings.Contains(message, "conflict"):
		return "workload action conflicted with a newer resource version"
	case strings.Contains(message, "not supported"):
		return "workload action is unsupported by the Kubernetes API"
	default:
		return "workload action failed"
	}
}
