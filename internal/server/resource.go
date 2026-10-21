package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/kube-haggis/kube-haggis/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const (
	maxDeleteCoordinateLength = 253
	maxDeleteResourceVersion  = 256
	maxDeleteConfirmation     = 32
	resourceConflictMessage   = "resource update conflicted with a newer resource version; review the current cluster state before retrying"
)

// getResource invokes the configured generic read seam or the Kubernetes service.
func (s *Server) getResource(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (*unstructured.Unstructured, error) {
	if s.resourceGet != nil {
		return s.resourceGet(ctx, contextName, group, version, resource, namespace, name, namespaced)
	}
	if s.kube == nil {
		return nil, fmt.Errorf("Kubernetes service is unavailable")
	}
	return s.kube.GetResource(ctx, contextName, group, version, resource, namespace, name, namespaced)
}

// applyResource invokes the configured generic full-object update seam or the Kubernetes service.
func (s *Server) applyResource(ctx context.Context, contextName, group, version, resource, namespace string, namespaced, dryRun bool, body []byte) (*unstructured.Unstructured, error) {
	if s.resourceApply != nil {
		return s.resourceApply(ctx, contextName, group, version, resource, namespace, namespaced, dryRun, body)
	}
	if s.kube == nil {
		return nil, fmt.Errorf("Kubernetes service is unavailable")
	}
	return s.kube.ApplyResource(ctx, contextName, group, version, resource, namespace, namespaced, dryRun, body)
}

// isResourceVersionConflict identifies resource-version conflicts without relying on raw error text in responses.
func isResourceVersionConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return apierrors.IsConflict(err) || strings.Contains(message, "resource version")
}

// resourceConflictResponse refreshes the current object and builds a safe bounded conflict payload.
func (s *Server) resourceConflictResponse(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool, body []byte) kube.ResourceConflictResponse {
	localObject := parseResourceDocument(body)
	response := kube.ResourceConflictResponse{
		Error:    resourceConflictMessage,
		Conflict: true,
		Summary: kube.ResourceConflictSummary{
			LocalResourceVersion: resourceObjectString(localObject, "resourceVersion"),
		},
	}

	currentName := strings.TrimSpace(name)
	if currentName == "" {
		currentName = resourceObjectString(localObject, "name")
	}
	currentNamespace := strings.TrimSpace(namespace)
	if namespaced && (currentNamespace == "" || currentNamespace == "all") {
		currentNamespace = resourceObjectString(localObject, "namespace")
	}
	if currentName == "" {
		return response
	}

	current, err := s.getResource(ctx, contextName, group, version, resource, currentNamespace, currentName, namespaced)
	if err != nil || current == nil {
		return response
	}

	safeObject := safeResourceConflictObject(current, resource)
	if len(safeObject) == 0 {
		return response
	}
	response.CurrentAvailable = true
	response.CurrentResourceVersion = current.GetResourceVersion()
	response.CurrentObject = safeObject
	response.Summary.CurrentResourceVersion = current.GetResourceVersion()
	response.Summary.ChangedSections = resourceConflictChangedSections(localObject, safeObject, resource)
	response.Summary.Truncated = len(response.Summary.ChangedSections) > kube.MaxResourceConflictSections
	if response.Summary.Truncated {
		response.Summary.ChangedSections = response.Summary.ChangedSections[:kube.MaxResourceConflictSections]
	}
	return response
}

// parseResourceDocument parses a YAML or JSON resource without retaining request text in a response.
func parseResourceDocument(body []byte) map[string]interface{} {
	var object map[string]interface{}
	if err := yaml.Unmarshal(body, &object); err != nil {
		return nil
	}
	return object
}

// resourceObjectString reads one safe metadata string from a parsed generic resource document.
func resourceObjectString(object map[string]interface{}, field string) string {
	if object == nil {
		return ""
	}
	metadata, found, err := unstructured.NestedMap(object, "metadata")
	if err != nil || !found {
		return ""
	}
	value, found := metadata[field]
	if !found {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// safeResourceConflictObject removes server-owned metadata and masks Secret values from a current object.
func safeResourceConflictObject(current *unstructured.Unstructured, resource string) map[string]interface{} {
	if current == nil {
		return nil
	}
	return safeResourceConflictMap(current.Object, resource, current.GetKind())
}

// safeResourceConflictMap deep-copies a generic object before applying conflict-response redactions.
func safeResourceConflictMap(object map[string]interface{}, resource, kind string) map[string]interface{} {
	if object == nil {
		return nil
	}
	copy := (&unstructured.Unstructured{Object: object}).DeepCopy().Object
	unstructured.RemoveNestedField(copy, "metadata", "managedFields")
	if strings.EqualFold(resource, "secrets") || strings.EqualFold(kind, "Secret") {
		for _, field := range []string{"data", "stringData"} {
			value, found := copy[field]
			if !found {
				continue
			}
			values, ok := value.(map[string]interface{})
			if !ok {
				copy[field] = "[redacted]"
				continue
			}
			masked := make(map[string]interface{}, len(values))
			for key := range values {
				masked[key] = "[redacted]"
			}
			copy[field] = masked
		}
	}
	return copy
}

// resourceConflictChangedSections compares only safe top-level sections and returns bounded names.
func resourceConflictChangedSections(local, current map[string]interface{}, resource string) []string {
	if local == nil || current == nil {
		return nil
	}
	localSafe := safeResourceConflictMap(local, resource, "")
	currentSafe := safeResourceConflictMap(current, resource, "")
	keys := make(map[string]struct{}, len(localSafe)+len(currentSafe))
	for key := range localSafe {
		keys[key] = struct{}{}
	}
	for key := range currentSafe {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	changed := make([]string, 0, len(ordered))
	for _, key := range ordered {
		left := conflictComparableSection(localSafe, key)
		right := conflictComparableSection(currentSafe, key)
		if !reflect.DeepEqual(left, right) {
			changed = append(changed, key)
		}
	}
	return changed
}

// conflictComparableSection ignores the expected resource-version-only difference in metadata summaries.
func conflictComparableSection(object map[string]interface{}, key string) interface{} {
	value, found := object[key]
	if !found || key != "metadata" {
		return value
	}
	metadata, ok := value.(map[string]interface{})
	if !ok {
		return value
	}
	copy := (&unstructured.Unstructured{Object: map[string]interface{}{"metadata": metadata}}).DeepCopy().Object["metadata"]
	metadataCopy, ok := copy.(map[string]interface{})
	if !ok {
		return value
	}
	delete(metadataCopy, "resourceVersion")
	delete(metadataCopy, "managedFields")
	return metadataCopy
}

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

// resourceMutationError converts generic apply failures into safe, actionable API errors.
func resourceMutationError(err error) error {
	message := strings.ToLower(err.Error())
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err), strings.Contains(message, "rbac"), strings.Contains(message, "forbidden"), strings.Contains(message, "unauthorized"):
		return fmt.Errorf("resource mutation was denied by Kubernetes RBAC")
	case apierrors.IsConflict(err), strings.Contains(message, "conflict"), strings.Contains(message, "resource version"):
		return fmt.Errorf("resource mutation conflicted with a newer resource version; refresh before retrying")
	case apierrors.IsMethodNotSupported(err), strings.Contains(message, "not supported"):
		return fmt.Errorf("resource mutation is not supported by this Kubernetes API server")
	case apierrors.IsNotFound(err), strings.Contains(message, "not found"):
		return fmt.Errorf("resource was not found; refresh before retrying")
	case strings.Contains(message, "decode "), strings.Contains(message, "parse "), strings.Contains(message, "invalid"), strings.Contains(message, "required"):
		return fmt.Errorf("resource mutation request is invalid")
	default:
		return fmt.Errorf("resource mutation failed; refresh and try again")
	}
}
