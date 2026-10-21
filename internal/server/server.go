// Package server exposes the KubeHaggis HTTP API and frontend shell.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kube-haggis/kube-haggis/internal/audit"
	"github.com/kube-haggis/kube-haggis/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// Server connects HTTP handlers to the Kubernetes service and frontend files.
type Server struct {
	kube           *kube.Service
	webDir         string
	logger         *slog.Logger
	audit          *audit.Store
	overview       func(context.Context, string) (kube.ClusterOverview, error)
	related        relatedLoader
	relatedReverse relatedLoader
	resourceSchema resourceSchemaLoader
	resourceGet    resourceGetLoader
	readPack       operationalReadPackLoader
	resourceApply  resourceApplyLoader
	resourceDelete resourceDeleteLoader
	workloadAction workloadActionLoader
	nodeAction     nodeActionLoader
	pvcOperations  pvcOperationsLoader
	pvcAction      pvcActionLoader
	reviewStoreMu  sync.Mutex
	reviewStore    *mutationReviewStore
	diagnosticsMu  sync.Mutex
	diagnostics    *diagnosticsStore
}

// relatedLoader describes the read-only relationship aggregation seam used by the HTTP handler.
type relatedLoader func(context.Context, string, string, string, string, string, string, bool) (kube.RelatedResourceList, error)

// resourceSchemaLoader describes the read-only normalized resource schema seam used by the HTTP handler.
type resourceSchemaLoader func(context.Context, string, string, string, string) (kube.ResourceSchema, error)

// resourceGetLoader describes the generic resource read seam used by HTTP handlers.
type resourceGetLoader func(context.Context, string, string, string, string, string, string, bool) (*unstructured.Unstructured, error)

// resourceApplyLoader describes the generic full-object update seam used by HTTP handlers.
type resourceApplyLoader func(context.Context, string, string, string, string, string, bool, bool, []byte) (*unstructured.Unstructured, error)

// resourceDeleteLoader describes the generic resource deletion seam used by HTTP handlers.
type resourceDeleteLoader func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error

// New creates an HTTP server for the KubeHaggis API.
func New(kubernetes *kube.Service, webDir string, logger *slog.Logger, auditStore *audit.Store) *Server {
	return &Server{kube: kubernetes, webDir: webDir, logger: logger, audit: auditStore, reviewStore: newMutationReviewStore(), diagnostics: newDiagnosticsStore()}
}

// Handler returns the complete API and frontend HTTP handler tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/readiness", s.handleReadiness)
	mux.HandleFunc("/api/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("/api/contexts", s.handleContexts)
	mux.HandleFunc("/api/contexts/reload", s.handleContextsReload)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/discovery", s.handleDiscovery)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/schema", s.handleSchema)
	mux.HandleFunc("/api/resources", s.handleResources)
	mux.HandleFunc("/api/resources/delete", s.handleBulkResourceDelete)
	mux.HandleFunc("/api/related", s.handleRelated)
	mux.HandleFunc("/api/related/reverse", s.handleRelatedReverse)
	mux.HandleFunc("/api/read-pack", s.handleOperationalReadPack)
	mux.HandleFunc("/api/watch", s.handleWatch)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/events/watch", s.handleEventWatch)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/pod/debug", s.handleEphemeralContainer)
	mux.HandleFunc("/api/pod/copy", s.handlePodCopy)
	mux.HandleFunc("/api/workload/action", s.handleWorkloadAction)
	mux.HandleFunc("/api/node/action", s.handleNodeAction)
	mux.HandleFunc("/api/pvc/operations", s.handlePVCOperations)
	mux.HandleFunc("/api/pvc/action", s.handlePVCAction)
	mux.Handle("/api/terminal", s.terminalHandler())
	mux.Handle("/api/port-forward", s.portForwardHandler())
	mux.HandleFunc("/api/resource", s.handleResource)
	mux.HandleFunc("/", s.handleFrontend)
	return s.withLogging(mux)
}

// handleHealth reports that the local HTTP process is alive.
func (s *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok", "name": "KubeHaggis", "liveness": "alive"})
}

// handleContexts returns kubeconfig contexts without exposing credentials.
func (s *Server) handleContexts(writer http.ResponseWriter, request *http.Request) {
	snapshot, err := s.kube.Contexts()
	payload := contextSnapshotPayload(snapshot)
	if err != nil {
		addSafeContextError(payload, writer, err)
	} else if snapshot.Kubeconfig.Error != "" {
		addSafeContextError(payload, writer, errors.New(snapshot.Kubeconfig.Error))
	}
	writeJSON(writer, http.StatusOK, payload)
}

// handleContextsReload forces a kubeconfig reload without returning source contents or credentials.
func (s *Server) handleContextsReload(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	snapshot, err := s.kube.ReloadContexts()
	s.invalidateMutationReviews()
	payload := contextSnapshotPayload(snapshot)
	if err != nil {
		addSafeContextError(payload, writer, err)
	} else if snapshot.Kubeconfig.Error != "" {
		addSafeContextError(payload, writer, errors.New(snapshot.Kubeconfig.Error))
	}
	writeJSON(writer, http.StatusOK, payload)
}

// addSafeContextError preserves context response status while adding only safe error metadata.
func addSafeContextError(payload map[string]interface{}, writer http.ResponseWriter, err error) {
	const contextErrorStatus = http.StatusBadGateway
	payload["error"] = safeErrorResponseMessage(err, contextErrorStatus)
	for key, value := range safeErrorFields(writer, err, contextErrorStatus) {
		payload[key] = value
	}
}

// contextSnapshotPayload builds the public context response from safe reload metadata.
func contextSnapshotPayload(snapshot kube.ContextSnapshot) map[string]interface{} {
	payload := map[string]interface{}{
		"currentContext": snapshot.CurrentContext,
		"contexts":       snapshot.Contexts,
		"sourceState":    snapshot.Kubeconfig.State,
		"source":         snapshot.Kubeconfig.Source,
		"revision":       snapshot.Kubeconfig.Revision,
		"reloaded":       snapshot.Kubeconfig.Reloaded,
	}
	if snapshot.Kubeconfig.Error != "" {
		payload["reloadError"] = snapshot.Kubeconfig.Error
	}
	return payload
}

// handleAudit returns the newest redacted local mutation entries.
func (s *Server) handleAudit(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.audit == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "local mutation history is unavailable"})
		return
	}
	entries, err := s.audit.List(request.Context(), parseLimit(request.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "local mutation history could not be read"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]interface{}{"items": redactedAuditEntries(entries)})
}

// redactedAuditEntries removes persisted detail text that may predate current audit safeguards.
func redactedAuditEntries(entries []audit.Entry) []audit.Entry {
	redacted := make([]audit.Entry, len(entries))
	copy(redacted, entries)
	for index := range redacted {
		redacted[index].Detail = redactMutationDetail(redacted[index].DryRun, redacted[index].Success, redacted[index].Detail)
	}
	return redacted
}

// handleOverview returns the aggregate model for one cluster context.
func (s *Server) handleOverview(writer http.ResponseWriter, request *http.Request) {
	contextName := request.URL.Query().Get("context")
	loadOverview := s.overview
	if loadOverview == nil {
		loadOverview = s.kube.Overview
	}
	overview, err := loadOverview(request.Context(), contextName)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, overview)
}

// handleDiscovery returns the complete API resource catalog and explicit discovery health for one context.
func (s *Server) handleDiscovery(writer http.ResponseWriter, request *http.Request) {
	contextName := request.URL.Query().Get("context")
	if parseBool(request.URL.Query().Get("refresh")) {
		if err := s.kube.InvalidateDiscovery(contextName); err != nil {
			writeError(writer, err)
			return
		}
	}
	catalog, err := s.kube.ResourceCatalog(request.Context(), contextName)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, catalog)
}

// handleSearch runs one bounded cross-resource search without returning Kubernetes error bodies.
func (s *Server) handleSearch(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	query := request.URL.Query()
	result, err := s.kube.Search(request.Context(), query.Get("context"), kube.SearchRequest{
		Query:         query.Get("query"),
		Group:         query.Get("group"),
		Kind:          query.Get("kind"),
		Resource:      query.Get("resource"),
		Namespace:     query.Get("namespace"),
		Name:          query.Get("name"),
		LabelSelector: query.Get("labelSelector"),
	})
	if err != nil {
		writeSearchError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

// handleSchema returns a read-only normalized resource schema or an explicit YAML fallback state.
func (s *Server) handleSchema(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	loadSchema := s.resourceSchema
	if loadSchema == nil {
		loadSchema = s.kube.ResourceSchema
	}
	resourceSchema, err := loadSchema(request.Context(), query.Get("context"), query.Get("group"), query.Get("version"), query.Get("resource"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, resourceSchema)
}

// handleResources returns a paginated generic resource collection.
func (s *Server) handleResources(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	limit := parseLimit(query.Get("limit"), 50, 500)
	namespaced := query.Get("namespaced") != "false"
	items, err := s.kube.ListResources(
		request.Context(),
		query.Get("context"),
		query.Get("group"),
		query.Get("version"),
		query.Get("resource"),
		query.Get("namespace"),
		namespaced,
		limit,
		query.Get("labelSelector"),
		query.Get("continue"),
	)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, items)
}

// handleRelated returns bounded, read-only relationships for one generic resource.
func (s *Server) handleRelated(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	loadRelated := s.related
	if loadRelated == nil {
		loadRelated = s.kube.RelatedResources
	}
	relationships, err := loadRelated(
		request.Context(),
		query.Get("context"),
		query.Get("group"),
		query.Get("version"),
		query.Get("resource"),
		query.Get("namespace"),
		query.Get("name"),
		query.Get("namespaced") != "false",
	)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, relationships)
}

// handleRelatedReverse returns the deferred broad reverse-owner relationship phase.
func (s *Server) handleRelatedReverse(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	loadRelated := s.relatedReverse
	if loadRelated == nil {
		loadRelated = s.kube.RelatedReverseResources
	}
	relationships, err := loadRelated(
		request.Context(),
		query.Get("context"),
		query.Get("group"),
		query.Get("version"),
		query.Get("resource"),
		query.Get("namespace"),
		query.Get("name"),
		query.Get("namespaced") != "false",
	)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, relationships)
}

// handleWatch streams generic resource changes as server-sent events.
func (s *Server) handleWatch(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	s.streamWatch(writer, request, "KubeHaggis watch connected", func(ctx context.Context) (watch.Interface, error) {
		return s.kube.WatchResources(
			ctx,
			query.Get("context"),
			query.Get("group"),
			query.Get("version"),
			query.Get("resource"),
			query.Get("namespace"),
			query.Get("namespaced") != "false",
			query.Get("resourceVersion"),
		)
	})
}

// handleEvents returns Events associated with one resource UID.
func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	events, resourceVersion, err := s.kube.EventsForResource(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("uid"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]interface{}{"items": events, "resourceVersion": resourceVersion})
}

// handleEventWatch streams Events associated with one resource UID.
func (s *Server) handleEventWatch(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	s.streamWatch(writer, request, "KubeHaggis Event watch connected", func(ctx context.Context) (watch.Interface, error) {
		return s.kube.WatchEventsForResource(ctx, query.Get("context"), query.Get("namespace"), query.Get("uid"), query.Get("resourceVersion"))
	})
}

// streamWatch adapts one Kubernetes watch to a safe, lifecycle-aware SSE stream.
func streamWatch(writer http.ResponseWriter, request *http.Request, connectedMessage string, open func(context.Context) (watch.Interface, error)) {
	streamWatchWithDiagnostics(nil, writer, request, connectedMessage, open)
}

// streamWatch runs a watch without server diagnostics for isolated handler tests.
func (s *Server) streamWatch(writer http.ResponseWriter, request *http.Request, connectedMessage string, open func(context.Context) (watch.Interface, error)) {
	streamWatchWithDiagnostics(s, writer, request, connectedMessage, open)
}

// streamWatchWithDiagnostics adapts one Kubernetes watch to SSE and bounded lifecycle telemetry.
func streamWatchWithDiagnostics(server *Server, writer http.ResponseWriter, request *http.Request, connectedMessage string, open func(context.Context) (watch.Interface, error)) {
	var watchHandle diagnosticWatchHandle
	operation := requestOperationClass(request.Method, request.URL.Path)
	telemetry := requestTelemetryFor(request)
	if server != nil {
		watchHandle = server.diagnosticStore().startWatch(telemetry.Context, telemetry.Resource, operation)
		defer func() {
			server.diagnosticStore().finishWatch(watchHandle, "closed", "", operation)
		}()
	}
	watcher, err := open(request.Context())
	if err != nil {
		if server != nil {
			category := safeErrorCategory(err)
			server.diagnosticStore().finishWatch(watchHandle, "error", category, operation)
			watchHandle = diagnosticWatchHandle{}
		}
		writeWatchOpenError(writer, err)
		return
	}
	defer watcher.Stop()

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		if server != nil {
			category := "stream-error"
			server.diagnosticStore().finishWatch(watchHandle, "error", category, operation)
			watchHandle = diagnosticWatchHandle{}
		}
		writeError(writer, fmt.Errorf("streaming is not supported by this server"))
		return
	}
	if _, err := writer.Write([]byte(": " + connectedMessage + "\n\n")); err != nil {
		if server != nil {
			server.diagnosticStore().finishWatch(watchHandle, "error", "stream-error", operation)
			watchHandle = diagnosticWatchHandle{}
		}
		return
	}
	flusher.Flush()

	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-watcher.ResultChan():
			if !open {
				writeWatchEvent(writer, flusher, map[string]interface{}{
					"type":  "CLOSED",
					"error": watchSignal("watch-closed"),
				})
				if server != nil {
					server.diagnosticStore().finishWatch(watchHandle, "closed", "watch-closed", operation)
					watchHandle = diagnosticWatchHandle{}
				}
				return
			}
			if event.Type == watch.Error {
				reason := watchErrorReason(event.Object)
				if !writeWatchEvent(writer, flusher, safeWatchErrorEvent(event.Object)) {
					if server != nil {
						server.diagnosticStore().finishWatch(watchHandle, "error", "stream-error", operation)
						watchHandle = diagnosticWatchHandle{}
					}
					return
				}
				if server != nil {
					server.diagnosticStore().finishWatch(watchHandle, "error", reason, operation)
					watchHandle = diagnosticWatchHandle{}
				}
				return
			}
			if !writeWatchEvent(writer, flusher, map[string]interface{}{"type": eventType(event.Type), "object": event.Object}) {
				if server != nil {
					server.diagnosticStore().finishWatch(watchHandle, "error", "stream-error", operation)
					watchHandle = diagnosticWatchHandle{}
				}
				return
			}
		}
	}
}

// writeWatchEvent writes one JSON SSE data frame and reports transport failure.
func writeWatchEvent(writer http.ResponseWriter, flusher http.Flusher, payload interface{}) bool {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	if _, err := writer.Write([]byte("data: " + string(encoded) + "\n\n")); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// safeWatchErrorEvent converts a Kubernetes watch error into a bounded browser signal.
func safeWatchErrorEvent(object runtime.Object) map[string]interface{} {
	reason := watchErrorReason(object)
	return map[string]interface{}{"type": "ERROR", "error": watchSignal(reason)}
}

// watchSignal builds a browser-safe watch failure payload without Kubernetes error text.
func watchSignal(reason string) map[string]interface{} {
	return map[string]interface{}{
		"reason": reason,
		"relist": reason == "resource-version-expired" || reason == "watch-error" || reason == "watch-closed",
	}
}

// watchErrorReason classifies a Kubernetes Status object without exposing its message.
func watchErrorReason(object runtime.Object) string {
	switch status := object.(type) {
	case *metav1.Status:
		if status.Code == http.StatusGone || status.Reason == metav1.StatusReasonExpired {
			return "resource-version-expired"
		}
		if status.Code == http.StatusForbidden || status.Reason == metav1.StatusReasonForbidden {
			return "permission-denied"
		}
	case *unstructured.Unstructured:
		code, _, _ := unstructured.NestedInt64(status.Object, "code")
		reason, _, _ := unstructured.NestedString(status.Object, "reason")
		if code == http.StatusGone || strings.EqualFold(reason, string(metav1.StatusReasonExpired)) {
			return "resource-version-expired"
		}
		if code == http.StatusForbidden || strings.EqualFold(reason, string(metav1.StatusReasonForbidden)) {
			return "permission-denied"
		}
	}
	return "watch-error"
}

// watchErrorReasonFromError classifies an initial watch failure for a safe HTTP response.
func watchErrorReasonFromError(err error) string {
	switch {
	case apierrors.IsResourceExpired(err):
		return "resource-version-expired"
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return "permission-denied"
	default:
		return "watch-error"
	}
}

// watchErrorMessage returns a stable operator-facing message for a watch failure.
func watchErrorMessage(reason string) string {
	switch reason {
	case "resource-version-expired":
		return "The Kubernetes watch resource version expired; the resource will be relisted."
	case "permission-denied":
		return "Kubernetes denied this watch; check RBAC permissions for the selected resource."
	default:
		return "The Kubernetes watch ended unexpectedly; the resource will be relisted."
	}
}

// writeWatchOpenError writes a safe classified response when a watch cannot start.
func writeWatchOpenError(writer http.ResponseWriter, err error) {
	reason := watchErrorReasonFromError(err)
	if reason == "permission-denied" {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache")
		writer.Header().Set("Connection", "keep-alive")
		if flusher, ok := writer.(http.Flusher); ok {
			writeWatchEvent(writer, flusher, map[string]interface{}{"type": "ERROR", "error": watchSignal(reason)})
			return
		}
	}
	status := http.StatusBadGateway
	switch reason {
	case "resource-version-expired":
		status = http.StatusGone
	case "permission-denied":
		status = http.StatusForbidden
	}
	payload := map[string]interface{}{
		"error":  watchErrorMessage(reason),
		"reason": reason,
		"relist": reason == "resource-version-expired" || reason == "watch-error",
	}
	writeJSON(writer, status, payload)
}

// handleLogs streams native Kubernetes Pod logs as text.
func (s *Server) handleLogs(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	tailLines := parseOptionalInt64(query.Get("tailLines"))
	stream, err := s.kube.PodLogs(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("pod"), query.Get("container"), parseBool(query.Get("follow")), parseBool(query.Get("previous")), tailLines)
	if err != nil {
		writeError(writer, err)
		return
	}
	defer stream.Close()
	telemetry := requestTelemetryFor(request)
	sessionID := s.diagnosticStore().startSession("logs", telemetry.Context, telemetry.Resource)
	defer func() {
		s.diagnosticStore().finishSession(sessionID, "", telemetry.Operation)
	}()
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	if flusher, ok := writer.(http.Flusher); ok {
		buffer := make([]byte, 8192)
		for {
			count, readErr := stream.Read(buffer)
			if count > 0 {
				_, _ = writer.Write(buffer[:count])
				flusher.Flush()
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) && !errors.Is(readErr, context.Canceled) {
					category := safeErrorCategory(readErr)
					s.diagnosticStore().finishSession(sessionID, category, telemetry.Operation)
					sessionID = ""
				}
				return
			}
		}
	}
	if _, copyErr := io.Copy(writer, stream); copyErr != nil && !errors.Is(copyErr, io.EOF) && !errors.Is(copyErr, context.Canceled) {
		category := safeErrorCategory(copyErr)
		s.diagnosticStore().finishSession(sessionID, category, telemetry.Operation)
		sessionID = ""
	}
}

// handleResource returns or mutates one generic resource object.
func (s *Server) handleResource(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	group := query.Get("group")
	version := query.Get("version")
	resource := query.Get("resource")
	namespace := query.Get("namespace")
	name := query.Get("name")
	namespaced := query.Get("namespaced") != "false"

	switch request.Method {
	case http.MethodGet:
		if name == "" {
			writeError(writer, fmt.Errorf("name is required"))
			return
		}
		item, err := s.getResource(request.Context(), query.Get("context"), group, version, resource, namespace, name, namespaced)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, item.Object)
	case http.MethodPost, http.MethodPatch:
		if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is not enabled for this request"})
			return
		}
		body, err := readBody(request)
		if err != nil {
			writeError(writer, err)
			return
		}
		dryRun := parseBool(query.Get("dryRun"))
		bodyHash, bodyObject, hashErr := sanitizedResourceBodyHash(body)
		if hashErr != nil {
			s.recordMutation(request, "apply", group, version, resource, namespace, name, dryRun, false, "resource mutation request validation failed")
			writeError(writer, hashErr)
			return
		}
		if validationErr := validateGenericApplyResourceVersion(name, bodyObject); validationErr != nil {
			s.recordMutation(request, "apply", group, version, resource, namespace, name, dryRun, false, "resource mutation request validation failed")
			writeError(writer, validationErr)
			return
		}
		binding := resourceApplyReviewBinding(s.mutationReviewContext(request), group, version, resource, namespace, name, namespaced, bodyObject, bodyHash)
		if !dryRun {
			if err := s.consumeMutationReview(request, binding); err != nil {
				s.recordMutation(request, "apply", group, version, resource, namespace, name, false, false, "mutation review was rejected")
				writeError(writer, err)
				return
			}
		}
		item, err := s.applyResource(request.Context(), query.Get("context"), group, version, resource, namespace, namespaced, dryRun, body)
		if err != nil {
			if isResourceVersionConflict(err) {
				conflict := s.resourceConflictResponse(request.Context(), query.Get("context"), group, version, resource, namespace, name, namespaced, body)
				s.recordMutation(request, "apply", group, version, resource, namespace, name, dryRun, false, "resource update conflicted with a newer resource version")
				writeJSON(writer, http.StatusConflict, conflict)
				return
			}
			s.recordMutation(request, "apply", group, version, resource, namespace, name, dryRun, false, err.Error())
			writeError(writer, resourceMutationError(err))
			return
		}
		if dryRun && !s.writeMutationReviewHeader(writer, binding) {
			return
		}
		s.recordMutation(request, "apply", group, version, resource, namespace, item.GetName(), dryRun, true, "")
		writeJSON(writer, http.StatusOK, item.Object)
	case http.MethodDelete:
		if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is not enabled for this request"})
			return
		}
		if name == "" {
			writeError(writer, fmt.Errorf("name is required"))
			return
		}
		deleteItem, validationErr := normalizeResourceDeleteItem(kube.ResourceDeleteItem{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: query.Get("expectedResourceVersion"),
		}, namespace, namespaced)
		if validationErr != nil {
			s.recordMutation(request, "delete", group, version, resource, namespace, name, parseBool(query.Get("dryRun")), false, "delete request validation failed")
			writeError(writer, validationErr)
			return
		}
		name = deleteItem.Name
		namespace = deleteItem.Namespace
		dryRun := parseBool(query.Get("dryRun"))
		if !dryRun && !parseBool(query.Get("confirmed")) {
			err := fmt.Errorf("destructive delete confirmation is required after a successful dry-run")
			s.recordMutation(request, "delete", group, version, resource, namespace, name, false, false, deleteAuditDetail(err))
			writeError(writer, err)
			return
		}
		if !dryRun && query.Get("confirmName") != name {
			err := fmt.Errorf("delete confirmation requires the exact resource name")
			s.recordMutation(request, "delete", group, version, resource, namespace, name, false, false, deleteAuditDetail(err))
			writeError(writer, err)
			return
		}
		expectedResourceVersion := deleteItem.ResourceVersion
		if expectedResourceVersion == "" {
			err := fmt.Errorf("resource version is required for safe deletion; refresh the resource and review again")
			s.recordMutation(request, "delete", group, version, resource, namespace, name, dryRun, false, deleteAuditDetail(err))
			writeError(writer, err)
			return
		}
		binding := resourceDeleteReviewBinding(s.mutationReviewContext(request), group, version, resource, deleteItem, namespaced, parseBool(query.Get("force")))
		if !dryRun {
			if err := s.consumeMutationReview(request, binding); err != nil {
				s.recordMutation(request, "delete", group, version, resource, namespace, name, false, false, "mutation review was rejected")
				writeError(writer, err)
				return
			}
		}
		err := s.deleteResource(request.Context(), query.Get("context"), group, version, resource, namespace, name, namespaced, kube.ResourceDeleteOptions{
			DryRun:                  dryRun,
			Force:                   parseBool(query.Get("force")),
			ExpectedResourceVersion: expectedResourceVersion,
		})
		if err != nil {
			s.recordMutation(request, "delete", group, version, resource, namespace, name, dryRun, false, deleteAuditDetail(err))
			writeError(writer, fmt.Errorf("%s", deleteErrorMessage(err)))
			return
		}
		if dryRun && !s.writeMutationReviewHeader(writer, binding) {
			return
		}
		detail := "resource deleted"
		status := "deleted"
		if dryRun {
			detail = "resource deletion dry-run passed"
			status = "dry-run passed"
		}
		s.recordMutation(request, "delete", group, version, resource, namespace, name, dryRun, true, detail)
		writeJSON(writer, http.StatusOK, map[string]interface{}{"status": status, "name": name, "namespace": namespace, "dryRun": dryRun})
	default:
		writer.Header().Set("Allow", "GET, POST, PATCH, DELETE")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// recordMutation writes a redacted mutation outcome without persisting request bodies.
func (s *Server) recordMutation(request *http.Request, operation, group, version, resource, namespace, name string, dryRun, success bool, detail string) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(request.Context(), audit.Entry{
		CreatedAt: time.Now().UTC(),
		Context:   request.URL.Query().Get("context"),
		Operation: operation,
		Group:     group,
		Version:   version,
		Resource:  resource,
		Namespace: namespace,
		Name:      name,
		DryRun:    dryRun,
		Success:   success,
		Detail:    redactMutationDetail(dryRun, success, detail),
	}); err != nil && s.logger != nil {
		s.logger.Warn("could not record mutation", "error", err)
	}
}

// redactMutationDetail converts mutation details into safe, bounded outcome text.
func redactMutationDetail(dryRun, success bool, detail string) string {
	if success {
		if dryRun {
			return "server-side dry-run passed"
		}
		return "mutation applied"
	}
	message := strings.ToLower(detail)
	switch {
	case strings.Contains(message, "rbac") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		return "operation denied by Kubernetes RBAC"
	case strings.Contains(message, "conflict") || strings.Contains(message, "resource version"):
		return "operation conflicted with a newer resource version"
	case strings.Contains(message, "unsupported") || strings.Contains(message, "not supported"):
		return "operation is unsupported by the Kubernetes API"
	case strings.Contains(message, "validation") || strings.Contains(message, "required") || strings.Contains(message, "invalid"):
		return "operation request validation failed"
	default:
		return "operation failed"
	}
}

// handleFrontend serves the built frontend and falls back to index.html for routes.
func (s *Server) handleFrontend(writer http.ResponseWriter, request *http.Request) {
	if s.webDir == "" {
		http.Error(writer, "KubeHaggis frontend is not built", http.StatusNotFound)
		return
	}
	if request.URL.Path == "/haggis.svg" {
		assetPath := filepath.Join(s.webDir, "..", "..", "haggis.svg")
		http.ServeFile(writer, request, assetPath)
		return
	}
	cleanPath := filepath.Clean(request.URL.Path)
	if cleanPath == "." || cleanPath == string(filepath.Separator) {
		cleanPath = "index.html"
	} else {
		cleanPath = strings.TrimPrefix(cleanPath, string(filepath.Separator))
	}
	candidate := filepath.Join(s.webDir, cleanPath)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		http.ServeFile(writer, request, candidate)
		return
	}
	http.ServeFile(writer, request, filepath.Join(s.webDir, "index.html"))
}

// withLogging adds concise request logging to the local server.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID := requestIDFor(request)
		request = request.WithContext(context.WithValue(request.Context(), requestIDContextKey{}, requestID))
		request.Header.Set("X-Request-ID", requestID)
		writer.Header().Set("X-Request-ID", requestID)
		captured := newRequestResponseWriter(writer)
		telemetry := requestTelemetryFor(request)
		defer func() {
			telemetry.Status = captured.Status()
			telemetry.Duration = time.Since(started)
			telemetry.ErrorCategory = captured.errorCategory
			if telemetry.ErrorCategory == "" {
				telemetry.ErrorCategory = errorCategoryForStatus(telemetry.Status)
			}
			s.diagnosticStore().recordRequest(telemetry)
			if s.logger != nil {
				s.logger.Info("http request",
					"request_id", telemetry.RequestID,
					"method", telemetry.Method,
					"status", telemetry.Status,
					"duration_ms", float64(telemetry.Duration)/float64(time.Millisecond),
					"context", telemetry.Context,
					"resource", telemetry.Resource,
					"operation", telemetry.Operation,
					"error_category", telemetry.ErrorCategory,
				)
			}
		}()
		next.ServeHTTP(captured, request)
	})
}

// readBody reads a bounded mutation document from a request.
func readBody(request *http.Request) ([]byte, error) {
	const maxBodySize = 2 << 20
	body, err := io.ReadAll(io.LimitReader(request.Body, maxBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodySize {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxBodySize)
	}
	return body, nil
}

// parseLimit parses a bounded list limit and falls back when it is invalid.
func parseLimit(value string, fallback, maximum int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}

// parseBool parses true-like query values.
func parseBool(value string) bool {
	return value == "true" || value == "1" || value == "yes"
}

// parseOptionalInt64 parses an optional positive integer query value.
func parseOptionalInt64(value string) *int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return nil
	}
	return &parsed
}

// eventType converts a Kubernetes watch event type to a wire-safe string.
func eventType(event watch.EventType) string {
	return string(event)
}

// writeJSON writes an encoded JSON response with the supplied status code.
func writeJSON(writer http.ResponseWriter, status int, value interface{}) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

// writeError converts a backend error into a consistent API error response.
func writeError(writer http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "conflict"):
		status = http.StatusConflict
	case strings.Contains(message, "drain blocked") || strings.Contains(message, "drain stopped"):
		status = http.StatusConflict
	case strings.Contains(message, "write mode") || strings.Contains(message, "denied by kubernetes rbac") || strings.Contains(message, "forbidden") || strings.Contains(message, "unauthorized"):
		status = http.StatusForbidden
	case strings.Contains(message, "not supported by this kubernetes api server"):
		status = http.StatusNotImplemented
	case strings.Contains(message, "not available on this kubernetes api server"):
		status = http.StatusNotImplemented
	case strings.Contains(message, "pvc resize is unsupported") || strings.Contains(message, "unsupported pvc operation") || strings.Contains(message, "volumesnapshot api is unavailable") || strings.Contains(message, "confirmation") || strings.Contains(message, "required") || strings.Contains(message, "not found") || strings.Contains(message, "not present in the discovery catalog") || strings.Contains(message, "scope contradicts") || strings.Contains(message, "cluster-scoped resource") || strings.Contains(message, "invalid") || strings.Contains(message, "unsupported workload action") || strings.Contains(message, "unsupported node action") || strings.Contains(message, "not a supported workload") || strings.Contains(message, "not supported for node") || strings.Contains(message, "must ") || strings.Contains(message, "exceeds") || strings.Contains(message, "ended after") || strings.Contains(message, "decode ") || strings.Contains(message, "parse "):
		status = http.StatusBadRequest
	case strings.Contains(message, "mutation review") || strings.Contains(message, "review handle"):
		status = http.StatusBadRequest
	}
	category := errorCategoryForStatus(status)
	payload := safeErrorFieldsForCategory(writer, category, status)
	payload["error"] = safeErrorResponseMessage(err, status)
	writeJSON(writer, status, payload)
}

// writeSearchError returns a fixed safe message for search failures and validation errors.
func writeSearchError(writer http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	category := "dependency-unavailable"
	message := "Global search is unavailable; retry the search."
	if strings.Contains(strings.ToLower(err.Error()), "search filter") || strings.Contains(strings.ToLower(err.Error()), "label selector") {
		status = http.StatusBadRequest
		category = "validation"
		message = "Search filters are invalid; check the label selector and filter lengths."
	}
	payload := safeErrorFieldsForCategory(writer, category, status)
	payload["error"] = message
	writeJSON(writer, status, payload)
}
