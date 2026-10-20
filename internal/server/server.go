// Package server exposes the KubeHaggis HTTP API and frontend shell.
package server

import (
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
	kube   *kube.Service
	webDir string
	logger *slog.Logger
	audit  *audit.Store
}

// New creates an HTTP server for the KubeHaggis API.
func New(kubernetes *kube.Service, webDir string, logger *slog.Logger, auditStore *audit.Store) *Server {
	return &Server{kube: kubernetes, webDir: webDir, logger: logger, audit: auditStore}
}

// Handler returns the complete API and frontend HTTP handler tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/contexts", s.handleContexts)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/discovery", s.handleDiscovery)
	mux.HandleFunc("/api/resources", s.handleResources)
	mux.HandleFunc("/api/watch", s.handleWatch)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/events/watch", s.handleEventWatch)
	mux.HandleFunc("/api/logs", s.handleLogs)
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
	contexts, current, err := s.kube.Contexts()
	if err != nil {
		writeJSON(writer, http.StatusOK, map[string]interface{}{"currentContext": "", "contexts": []kube.ContextSummary{}, "error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]interface{}{"currentContext": current, "contexts": contexts})
}

// handleAudit returns the newest redacted local mutation entries.
func (s *Server) handleAudit(writer http.ResponseWriter, request *http.Request) {
	entries, err := s.audit.List(request.Context(), parseLimit(request.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]interface{}{"items": entries})
}

// handleOverview returns the aggregate model for one cluster context.
func (s *Server) handleOverview(writer http.ResponseWriter, request *http.Request) {
	contextName := request.URL.Query().Get("context")
	overview, err := s.kube.Overview(request.Context(), contextName)
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
		err := s.kube.DeleteResource(request.Context(), query.Get("context"), group, version, resource, namespace, name, namespaced, parseBool(query.Get("dryRun")), parseBool(query.Get("force")))
		if err != nil {
			s.recordMutation(request, "delete", group, version, resource, namespace, name, parseBool(query.Get("dryRun")), false, err.Error())
			writeError(writer, err)
			return
		}
		s.recordMutation(request, "delete", group, version, resource, namespace, name, parseBool(query.Get("dryRun")), true, "")
		writeJSON(writer, http.StatusOK, map[string]string{"status": "deleted", "name": name})
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
		Detail:    detail,
	}); err != nil && s.logger != nil {
		s.logger.Warn("could not record mutation", "error", err)
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
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "not found") {
		status = http.StatusBadRequest
	}
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}
