// Package server exposes the KubeHaggis HTTP API and frontend shell.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kube-haggis/kube-haggis/internal/audit"
	"github.com/kube-haggis/kube-haggis/internal/kube"
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
	resourceSchema resourceSchemaLoader
	resourceDelete resourceDeleteLoader
	workloadAction workloadActionLoader
	nodeAction     nodeActionLoader
	pvcOperations  pvcOperationsLoader
	pvcAction      pvcActionLoader
}

// relatedLoader describes the read-only relationship aggregation seam used by the HTTP handler.
type relatedLoader func(context.Context, string, string, string, string, string, string, bool) (kube.RelatedResourceList, error)

// resourceSchemaLoader describes the read-only CRD schema seam used by the HTTP handler.
type resourceSchemaLoader func(context.Context, string, string, string, string) (kube.ResourceSchema, error)

// resourceDeleteLoader describes the generic resource deletion seam used by HTTP handlers.
type resourceDeleteLoader func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error

// New creates an HTTP server for the KubeHaggis API.
func New(kubernetes *kube.Service, webDir string, logger *slog.Logger, auditStore *audit.Store) *Server {
	return &Server{kube: kubernetes, webDir: webDir, logger: logger, audit: auditStore}
}

// Handler returns the complete API and frontend HTTP handler tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/contexts", s.handleContexts)
	mux.HandleFunc("/api/contexts/reload", s.handleContextsReload)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/discovery", s.handleDiscovery)
	mux.HandleFunc("/api/schema", s.handleSchema)
	mux.HandleFunc("/api/resources", s.handleResources)
	mux.HandleFunc("/api/resources/delete", s.handleBulkResourceDelete)
	mux.HandleFunc("/api/related", s.handleRelated)
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
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok", "name": "KubeHaggis"})
}

// handleContexts returns kubeconfig contexts without exposing credentials.
func (s *Server) handleContexts(writer http.ResponseWriter, request *http.Request) {
	snapshot, err := s.kube.Contexts()
	payload := contextSnapshotPayload(snapshot)
	if err != nil {
		payload["error"] = err.Error()
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
	payload := contextSnapshotPayload(snapshot)
	if err != nil {
		payload["error"] = err.Error()
	}
	writeJSON(writer, http.StatusOK, payload)
}

// contextSnapshotPayload builds the public context response from safe reload metadata.
func contextSnapshotPayload(snapshot kube.ContextSnapshot) map[string]interface{} {
	payload := map[string]interface{}{
		"currentContext": snapshot.CurrentContext,
		"contexts":       snapshot.Contexts,
		"sourceState":    snapshot.Kubeconfig.State,
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

// handleDiscovery returns preferred API resource definitions for one context.
func (s *Server) handleDiscovery(writer http.ResponseWriter, request *http.Request) {
	contextName := request.URL.Query().Get("context")
	definitions, err := s.kube.Discovery(request.Context(), contextName)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]interface{}{"items": definitions})
}

// handleSchema returns a read-only structural CRD schema or an explicit YAML fallback state.
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

// handleWatch streams generic resource changes as server-sent events.
func (s *Server) handleWatch(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	watcher, err := s.kube.WatchResources(
		request.Context(),
		query.Get("context"),
		query.Get("group"),
		query.Get("version"),
		query.Get("resource"),
		query.Get("namespace"),
		query.Get("namespaced") != "false",
		query.Get("resourceVersion"),
	)
	if err != nil {
		writeError(writer, err)
		return
	}
	defer watcher.Stop()

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, fmt.Errorf("streaming is not supported by this server"))
		return
	}
	_, _ = writer.Write([]byte(": KubeHaggis watch connected\n\n"))
	flusher.Flush()

	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-watcher.ResultChan():
			if !open {
				return
			}
			payload := map[string]interface{}{"type": eventType(event.Type), "object": event.Object}
			encoded, encodeErr := json.Marshal(payload)
			if encodeErr != nil {
				continue
			}
			_, _ = writer.Write([]byte("data: " + string(encoded) + "\n\n"))
			flusher.Flush()
		}
	}
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
	watcher, err := s.kube.WatchEventsForResource(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("uid"), query.Get("resourceVersion"))
	if err != nil {
		writeError(writer, err)
		return
	}
	defer watcher.Stop()

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, fmt.Errorf("streaming is not supported by this server"))
		return
	}
	_, _ = writer.Write([]byte(": KubeHaggis Event watch connected\n\n"))
	flusher.Flush()

	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-watcher.ResultChan():
			if !open {
				return
			}
			payload := map[string]interface{}{"type": eventType(event.Type), "object": event.Object}
			encoded, encodeErr := json.Marshal(payload)
			if encodeErr != nil {
				continue
			}
			_, _ = writer.Write([]byte("data: " + string(encoded) + "\n\n"))
			flusher.Flush()
		}
	}
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
				return
			}
		}
	}
	_, _ = io.Copy(writer, stream)
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
		item, err := s.kube.GetResource(request.Context(), query.Get("context"), group, version, resource, namespace, name, namespaced)
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
		item, err := s.kube.ApplyResource(request.Context(), query.Get("context"), group, version, resource, namespace, namespaced, parseBool(query.Get("dryRun")), body)
		if err != nil {
			s.recordMutation(request, "apply", group, version, resource, namespace, name, parseBool(query.Get("dryRun")), false, err.Error())
			writeError(writer, err)
			return
		}
		s.recordMutation(request, "apply", group, version, resource, namespace, item.GetName(), parseBool(query.Get("dryRun")), true, "")
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
		next.ServeHTTP(writer, request)
		if s.logger != nil {
			s.logger.Info("http request", "method", request.Method, "path", request.URL.Path, "duration", time.Since(started).String())
		}
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
	case strings.Contains(message, "pvc resize is unsupported") || strings.Contains(message, "unsupported pvc operation") || strings.Contains(message, "volumesnapshot api is unavailable") || strings.Contains(message, "confirmation") || strings.Contains(message, "required") || strings.Contains(message, "not found") || strings.Contains(message, "invalid") || strings.Contains(message, "unsupported workload action") || strings.Contains(message, "unsupported node action") || strings.Contains(message, "not a supported workload") || strings.Contains(message, "not supported for node") || strings.Contains(message, "must ") || strings.Contains(message, "exceeds") || strings.Contains(message, "ended after") || strings.Contains(message, "decode ") || strings.Contains(message, "parse "):
		status = http.StatusBadRequest
	}
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}
