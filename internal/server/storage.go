package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kube-haggis/kube-haggis/internal/kube"
)

// pvcOperationsLoader describes the read-only PVC capability seam used by the HTTP handler.
type pvcOperationsLoader func(context.Context, string, string, string) (kube.PVCOperationInfo, error)

// pvcActionLoader describes the native PVC mutation seam used by the HTTP handler.
type pvcActionLoader func(context.Context, string, string, string, kube.PVCActionRequest) (kube.PVCActionResult, error)

// pvcActionReviewBinding binds a normalized PVC operation to its exact resource version and action parameters.
func pvcActionReviewBinding(contextName, namespace, name string, request kube.PVCActionRequest) reviewBinding {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.ExpectedResourceVersion = strings.TrimSpace(request.ExpectedResourceVersion)
	request.Storage = strings.TrimSpace(request.Storage)
	request.SnapshotName = strings.TrimSpace(request.SnapshotName)
	request.SnapshotClassName = strings.TrimSpace(request.SnapshotClassName)
	request.DryRun = false
	request.Confirmed = false
	return reviewBinding{
		Context:                 strings.TrimSpace(contextName),
		Version:                 "v1",
		Resource:                "persistentvolumeclaims",
		Namespace:               strings.TrimSpace(namespace),
		Name:                    strings.TrimSpace(name),
		Namespaced:              true,
		ExpectedResourceVersion: request.ExpectedResourceVersion,
		Action:                  "pvc-action",
		ParametersHash:          hashReviewParameters(request),
		ConfirmationScope:       request.Action,
	}
}

// handlePVCOperations returns resize and VolumeSnapshot capability states for one PVC.
func (s *Server) handlePVCOperations(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	query := request.URL.Query()
	loadOperations := s.pvcOperations
	if loadOperations == nil {
		if s.kube == nil {
			writeError(writer, fmt.Errorf("Kubernetes service is unavailable"))
			return
		}
		loadOperations = s.kube.PVCOperations
	}
	info, err := loadOperations(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("name"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, info)
}

// handlePVCAction runs one explicitly selected PVC operation after the write-mode guard.
func (s *Server) handlePVCAction(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required for PVC operations"})
		return
	}
	body, err := readBody(request)
	if err != nil {
		writeError(writer, err)
		return
	}
	var payload kube.PVCActionRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, fmt.Errorf("decode PVC operation request: %w", err))
		return
	}
	query := request.URL.Query()
	contextName := s.mutationReviewContext(request)
	binding := pvcActionReviewBinding(contextName, query.Get("namespace"), query.Get("name"), payload)
	if !payload.DryRun {
		if err := s.consumeMutationReview(request, binding); err != nil {
			s.recordMutation(request, pvcAuditOperation(payload.Action), "", "v1", "persistentvolumeclaims", query.Get("namespace"), query.Get("name"), false, false, "mutation review was rejected")
			writeError(writer, err)
			return
		}
	}
	loadAction := s.pvcAction
	if loadAction == nil {
		if s.kube == nil {
			writeError(writer, fmt.Errorf("Kubernetes service is unavailable"))
			return
		}
		loadAction = s.kube.PVCAction
	}
	result, err := loadAction(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("name"), payload)
	operation := pvcAuditOperation(payload.Action)
	if err != nil {
		s.recordMutation(request, operation, "", "v1", "persistentvolumeclaims", query.Get("namespace"), query.Get("name"), payload.DryRun, false, pvcAuditDetail(err))
		writeError(writer, err)
		return
	}
	if payload.DryRun && result.Status == "dry-run passed" && !s.writeMutationReviewHeader(writer, binding) {
		return
	}
	detail := "PVC operation applied"
	if payload.DryRun {
		detail = "PVC operation dry-run passed"
	}
	s.recordMutation(request, operation, "", "v1", "persistentvolumeclaims", query.Get("namespace"), query.Get("name"), payload.DryRun, true, detail)
	writeJSON(writer, http.StatusOK, result)
}

// pvcAuditOperation returns a bounded operation name without persisting request fields.
func pvcAuditOperation(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "resize", "snapshot":
		return "pvc-" + strings.ToLower(strings.TrimSpace(action))
	default:
		return "pvc-operation"
	}
}

// pvcAuditDetail categorizes an error without persisting storage, class, or snapshot names.
func pvcAuditDetail(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "rbac") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		return "PVC operation denied by Kubernetes RBAC"
	case strings.Contains(message, "conflict") || strings.Contains(message, "resource version"):
		return "PVC operation conflicted with a newer resource version"
	case strings.Contains(message, "not available") || strings.Contains(message, "unsupported") || strings.Contains(message, "not supported"):
		return "PVC operation is unsupported by the Kubernetes API or storage configuration"
	default:
		return "PVC operation failed"
	}
}
