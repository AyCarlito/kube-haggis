package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kube-haggis/kube-haggis/internal/kube"
)

const (
	maxDeleteCoordinateLength = 253
	maxDeleteResourceVersion  = 256
	maxDeleteConfirmation     = 32
)

// deleteResource invokes the configured generic delete seam or the Kubernetes service.
func (s *Server) deleteResource(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool, options kube.ResourceDeleteOptions) error {
	if s.resourceDelete != nil {
		return s.resourceDelete(ctx, contextName, group, version, resource, namespace, name, namespaced, options)
	}
	if s.kube == nil {
		return fmt.Errorf("Kubernetes service is unavailable")
	}
	return s.kube.DeleteResourceWithOptions(ctx, contextName, group, version, resource, namespace, name, namespaced, options)
}

// handleBulkResourceDelete reviews and executes bounded generic resource deletions.
func (s *Server) handleBulkResourceDelete(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required for bulk deletion"})
		return
	}
	body, err := readBody(request)
	if err != nil {
		writeError(writer, err)
		return
	}
	var payload kube.BulkDeleteRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		validationErr := fmt.Errorf("invalid bulk delete request")
		s.recordMutation(request, "bulk-delete", request.URL.Query().Get("group"), request.URL.Query().Get("version"), request.URL.Query().Get("resource"), request.URL.Query().Get("namespace"), "", payload.DryRun, false, "bulk delete request validation failed")
		writeError(writer, validationErr)
		return
	}
	query := request.URL.Query()
	namespaced := query.Get("namespaced") != "false"
	items, validationErr := validateBulkDeleteRequest(payload, query.Get("namespace"), namespaced)
	if validationErr != nil {
		s.recordMutation(request, "bulk-delete", query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), "", payload.DryRun, false, "bulk delete request validation failed")
		writeError(writer, validationErr)
		return
	}
	if !payload.DryRun {
		expectedConfirmation := fmt.Sprintf("DELETE %d", len(items))
		if !payload.Confirmed || strings.TrimSpace(payload.Confirmation) != expectedConfirmation {
			confirmationErr := fmt.Errorf("bulk delete confirmation is required; type %q after reviewing the dry-run", expectedConfirmation)
			s.recordMutation(request, "bulk-delete", query.Get("group"), query.Get("version"), query.Get("resource"), query.Get("namespace"), "", false, false, "bulk delete confirmation was rejected")
			writeError(writer, confirmationErr)
			return
		}
	}

	response := kube.BulkDeleteResponse{
		Resource:  query.Get("resource"),
		Namespace: query.Get("namespace"),
		DryRun:    payload.DryRun,
		Total:     len(items),
		Items:     make([]kube.BulkDeleteItemResult, 0, len(items)),
	}
	for _, item := range items {
		result := kube.BulkDeleteItemResult{Name: item.Name, Namespace: item.Namespace}
		err := s.deleteResource(request.Context(), query.Get("context"), query.Get("group"), query.Get("version"), query.Get("resource"), item.Namespace, item.Name, namespaced, kube.ResourceDeleteOptions{
			DryRun:                  payload.DryRun,
			Force:                   payload.Force,
			ExpectedResourceVersion: item.ResourceVersion,
		})
		if err != nil {
			result.Status = "failed"
			result.Error = deleteErrorMessage(err)
			response.Failed++
			s.recordMutation(request, "bulk-delete", query.Get("group"), query.Get("version"), query.Get("resource"), item.Namespace, item.Name, payload.DryRun, false, deleteAuditDetail(err))
		} else {
			result.Success = true
			result.Status = "deleted"
			if payload.DryRun {
				result.Status = "dry-run passed"
			}
			response.Succeeded++
			detail := "resource deleted"
			if payload.DryRun {
				detail = "resource deletion dry-run passed"
			}
			s.recordMutation(request, "bulk-delete", query.Get("group"), query.Get("version"), query.Get("resource"), item.Namespace, item.Name, payload.DryRun, true, detail)
		}
		response.Items = append(response.Items, result)
	}
	response.Status = bulkDeleteStatus(response)
	writeJSON(writer, http.StatusOK, response)
}

// validateBulkDeleteRequest validates and normalizes every selected resource before mutation.
func validateBulkDeleteRequest(request kube.BulkDeleteRequest, namespace string, namespaced bool) ([]kube.ResourceDeleteItem, error) {
	if len(request.Items) < 1 {
		return nil, fmt.Errorf("at least one resource must be selected for bulk deletion")
	}
	if len(request.Items) > kube.MaxBulkResourceDeletes {
		return nil, fmt.Errorf("bulk deletion is limited to %d resources per review", kube.MaxBulkResourceDeletes)
	}
	if len(request.Confirmation) > maxDeleteConfirmation || strings.ContainsAny(request.Confirmation, "\x00\r\n\t") {
		return nil, fmt.Errorf("bulk delete confirmation is invalid")
	}
	if request.Force {
		return nil, fmt.Errorf("force deletion is not supported for bulk deletion; review each resource individually")
	}
	normalized := make([]kube.ResourceDeleteItem, 0, len(request.Items))
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		clean, err := normalizeResourceDeleteItem(item, namespace, namespaced)
		if err != nil {
			return nil, err
		}
		key := clean.Namespace + "\x00" + clean.Name
		if _, found := seen[key]; found {
			return nil, fmt.Errorf("resource %q is selected more than once", clean.Name)
		}
		seen[key] = struct{}{}
		normalized = append(normalized, clean)
	}
	return normalized, nil
}

// normalizeResourceDeleteItem bounds safe coordinates and resolves the effective namespace.
func normalizeResourceDeleteItem(item kube.ResourceDeleteItem, namespace string, namespaced bool) (kube.ResourceDeleteItem, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Namespace = strings.TrimSpace(item.Namespace)
	item.ResourceVersion = strings.TrimSpace(item.ResourceVersion)
	if item.Name == "" {
		return kube.ResourceDeleteItem{}, fmt.Errorf("resource name is required for deletion")
	}
	if len(item.Name) > maxDeleteCoordinateLength || strings.ContainsAny(item.Name, "\x00\r\n\t") {
		return kube.ResourceDeleteItem{}, fmt.Errorf("resource name is invalid for deletion")
	}
	if item.ResourceVersion == "" {
		return kube.ResourceDeleteItem{}, fmt.Errorf("resource version is required for safe deletion; refresh the resource and review again")
	}
	if len(item.ResourceVersion) > maxDeleteResourceVersion || strings.ContainsAny(item.ResourceVersion, "\x00\r\n\t") {
		return kube.ResourceDeleteItem{}, fmt.Errorf("resource version is invalid for deletion")
	}
	if !namespaced {
		item.Namespace = ""
		return item, nil
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || namespace == "all" {
		if item.Namespace == "" {
			return kube.ResourceDeleteItem{}, fmt.Errorf("namespace is required for each selected namespaced resource")
		}
	} else if item.Namespace == "" {
		item.Namespace = namespace
	} else if item.Namespace != namespace {
		return kube.ResourceDeleteItem{}, fmt.Errorf("selected resource namespace %q does not match the active namespace", item.Namespace)
	}
	if item.Namespace == "all" {
		return kube.ResourceDeleteItem{}, fmt.Errorf("resource namespace must identify one namespace, not all namespaces")
	}
	if len(item.Namespace) > maxDeleteCoordinateLength || strings.ContainsAny(item.Namespace, "\x00\r\n\t") {
		return kube.ResourceDeleteItem{}, fmt.Errorf("resource namespace is invalid for deletion")
	}
	return item, nil
}

// bulkDeleteStatus summarizes whether every selected deletion passed or failed.
func bulkDeleteStatus(response kube.BulkDeleteResponse) string {
	if response.Failed == 0 {
		if response.DryRun {
			return "dry-run passed"
		}
		return "deleted"
	}
	if response.Succeeded == 0 {
		return "failed"
	}
	return "partial"
}

// deleteErrorMessage converts Kubernetes delete failures into safe, actionable API errors.
func deleteErrorMessage(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "write mode"):
		return "write mode is required for deletion"
	case strings.Contains(message, "rbac") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		return "delete denied by Kubernetes RBAC"
	case strings.Contains(message, "resource version") || strings.Contains(message, "conflict"):
		return "delete conflicted with a newer resource version; refresh and review again"
	case strings.Contains(message, "not found"):
		return "resource was not found; refresh the list before retrying"
	case strings.Contains(message, "not supported"):
		return "deletion is not supported by this Kubernetes API"
	case strings.Contains(message, "confirmation") || strings.Contains(message, "type "):
		return "destructive delete confirmation was not accepted"
	default:
		return "resource deletion failed; refresh and review again"
	}
}

// deleteAuditDetail categorizes a deletion failure without persisting request data.
func deleteAuditDetail(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "confirmation") || strings.Contains(message, "type "):
		return "delete confirmation was rejected"
	case strings.Contains(message, "resource version") || strings.Contains(message, "conflict"):
		return "delete conflicted with a newer resource version"
	case strings.Contains(message, "rbac") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		return "delete denied by Kubernetes RBAC"
	case strings.Contains(message, "not found"):
		return "resource was not found before deletion"
	case strings.Contains(message, "not supported"):
		return "delete is unsupported by the Kubernetes API"
	case strings.Contains(message, "required") || strings.Contains(message, "invalid"):
		return "delete request validation failed"
	default:
		return "delete failed"
	}
}
