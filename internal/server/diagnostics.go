package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/kube-haggis/kube-haggis/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	maxRequestIDLength        = 64
	maxDiagnosticErrors       = 32
	maxDiagnosticWatchEvents  = 32
	maxDiagnosticWatches      = 32
	maxDiagnosticSessions     = 128
	maxDiagnosticText         = 128
	readinessDiscoveryTimeout = 2 * time.Second
	readinessAuditTimeout     = 250 * time.Millisecond
)

var requestIDFallbackSequence atomic.Uint64

// requestIDContextKey stores the safe correlation ID in a request context.
type requestIDContextKey struct{}

// requestTelemetry contains only bounded request metadata suitable for logs and counters.
type requestTelemetry struct {
	RequestID     string
	Method        string
	Status        int
	Duration      time.Duration
	Context       string
	Resource      string
	Operation     string
	ErrorCategory string
}

// requestResponseWriter captures response status and bytes while preserving streaming interfaces.
type requestResponseWriter struct {
	http.ResponseWriter
	status        int
	bytes         int64
	wroteHeader   bool
	hijacked      bool
	errorCategory string
}

// newRequestResponseWriter creates a response writer that preserves the underlying writer's capabilities.
func newRequestResponseWriter(writer http.ResponseWriter) *requestResponseWriter {
	return &requestResponseWriter{ResponseWriter: writer}
}

// Status returns the final captured HTTP status, defaulting to success when no body was written.
func (writer *requestResponseWriter) Status() int {
	if writer.status == 0 {
		return http.StatusOK
	}
	return writer.status
}

// BytesWritten returns the number of response bytes passed to the underlying writer.
func (writer *requestResponseWriter) BytesWritten() int64 {
	return writer.bytes
}

// RequestID returns the correlation ID assigned by the request middleware.
func (writer *requestResponseWriter) RequestID() string {
	return writer.Header().Get("X-Request-ID")
}

// SetErrorCategory records a safe error category before the middleware emits telemetry.
func (writer *requestResponseWriter) SetErrorCategory(category string) {
	writer.errorCategory = normalizeDiagnosticCategory(category)
}

// WriteHeader captures the final response status without interfering with informational responses.
func (writer *requestResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		writer.ResponseWriter.WriteHeader(status)
		return
	}
	if writer.wroteHeader || writer.hijacked {
		return
	}
	writer.status = status
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(status)
}

// Write captures response bytes and commits an implicit successful status when needed.
func (writer *requestResponseWriter) Write(payload []byte) (int, error) {
	if !writer.wroteHeader && !writer.hijacked {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.hijacked {
		return 0, http.ErrNotSupported
	}
	count, err := writer.ResponseWriter.Write(payload)
	writer.bytes += int64(count)
	return count, err
}

// Flush forwards an SSE or streaming flush while preserving status capture.
func (writer *requestResponseWriter) Flush() {
	if !writer.wroteHeader && !writer.hijacked {
		writer.WriteHeader(http.StatusOK)
	}
	flusher, ok := writer.ResponseWriter.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

// Hijack forwards WebSocket upgrades without writing a second HTTP response.
func (writer *requestResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	connection, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	writer.status = http.StatusSwitchingProtocols
	writer.wroteHeader = true
	writer.hijacked = true
	return connection, buffered, nil
}

// ReadFrom preserves efficient streaming copies and captures their byte count.
func (writer *requestResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	if !writer.wroteHeader && !writer.hijacked {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.hijacked {
		return 0, http.ErrNotSupported
	}
	readerFrom, ok := writer.ResponseWriter.(io.ReaderFrom)
	if ok {
		count, err := readerFrom.ReadFrom(source)
		writer.bytes += count
		return count, err
	}
	count, err := io.Copy(writer.ResponseWriter, source)
	writer.bytes += count
	return count, err
}

// Unwrap exposes the original writer to http.ResponseController and compatible middleware.
func (writer *requestResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// Push forwards optional HTTP/2 server push support without requiring it.
func (writer *requestResponseWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := writer.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

// diagnosticErrorEvent is one bounded safe error occurrence without its message.
type diagnosticErrorEvent struct {
	At        time.Time `json:"at"`
	Status    int       `json:"status,omitempty"`
	Category  string    `json:"category"`
	Operation string    `json:"operation"`
}

// diagnosticWatchEvent is one bounded lifecycle transition for a server-side watch.
type diagnosticWatchEvent struct {
	At        time.Time `json:"at"`
	Context   string    `json:"context,omitempty"`
	Resource  string    `json:"resource,omitempty"`
	State     string    `json:"state"`
	Reconnect bool      `json:"reconnect,omitempty"`
	Error     string    `json:"errorCategory,omitempty"`
}

// diagnosticWatchStatus stores the latest safe state for one context and GVR watch key.
type diagnosticWatchStatus struct {
	Context    string
	Resource   string
	State      string
	LastAt     time.Time
	Error      string
	Reconnects uint64
	Active     bool
}

// diagnosticSession tracks an active native or watch stream without target identity.
type diagnosticSession struct {
	Kind     string
	Context  string
	Resource string
}

// diagnosticWatchHandle binds lifecycle updates to one bounded watch summary.
type diagnosticWatchHandle struct {
	SessionID string
	Key       string
	Context   string
	Resource  string
	Tracked   bool
}

// diagnosticsStore retains bounded request counters and safe lifecycle summaries in memory.
type diagnosticsStore struct {
	mu              sync.RWMutex
	startedAt       time.Time
	nextSessionID   uint64
	requestTotal    uint64
	errorTotal      uint64
	durationTotal   time.Duration
	maxDuration     time.Duration
	statusCounts    map[int]uint64
	operationCounts map[string]uint64
	errorCounts     map[string]uint64
	recentErrors    []diagnosticErrorEvent
	sessions        map[string]diagnosticSession
	activeSessions  map[string]uint64
	watches         map[string]diagnosticWatchStatus
	recentWatches   []diagnosticWatchEvent
}

// diagnosticsKubeconfig is the browser-safe kubeconfig source summary.
type diagnosticsKubeconfig struct {
	Source   string `json:"source"`
	State    string `json:"state"`
	Revision uint64 `json:"revision"`
}

// diagnosticsContext is the browser-safe selected-context summary.
type diagnosticsContext struct {
	Current  string `json:"current"`
	Selected string `json:"selected"`
	State    string `json:"state"`
}

// diagnosticsAudit is the browser-safe audit-store availability summary.
type diagnosticsAudit struct {
	State string `json:"state"`
}

// diagnosticsSessions is the bounded count of active native and watch sessions.
type diagnosticsSessions struct {
	Terminal    uint64 `json:"terminal"`
	Logs        uint64 `json:"logs"`
	PortForward uint64 `json:"portForward"`
	Watch       uint64 `json:"watch"`
}

// diagnosticsRequests contains bounded request totals, status counts, and timings.
type diagnosticsRequests struct {
	Total                       uint64            `json:"total"`
	Errors                      uint64            `json:"errors"`
	AverageDurationMilliseconds float64           `json:"averageDurationMilliseconds"`
	MaxDurationMilliseconds     float64           `json:"maxDurationMilliseconds"`
	Statuses                    map[string]uint64 `json:"statuses"`
	Operations                  map[string]uint64 `json:"operations"`
	ErrorCategories             map[string]uint64 `json:"errorCategories"`
}

// diagnosticsWatches contains active watch count, current states, and recent transitions.
type diagnosticsWatches struct {
	Active  uint64                 `json:"active"`
	Current []diagnosticWatchView  `json:"current"`
	Recent  []diagnosticWatchEvent `json:"recent"`
}

// diagnosticWatchView is a JSON-safe view of one current watch state.
type diagnosticWatchView struct {
	Context    string    `json:"context,omitempty"`
	Resource   string    `json:"resource,omitempty"`
	State      string    `json:"state"`
	LastAt     time.Time `json:"lastAt"`
	Error      string    `json:"errorCategory,omitempty"`
	Reconnects uint64    `json:"reconnects"`
	Active     bool      `json:"active"`
}

// diagnosticsResponse is the complete safe local diagnostics payload.
type diagnosticsResponse struct {
	GeneratedAt   time.Time                   `json:"generatedAt"`
	UptimeSeconds int64                       `json:"uptimeSeconds"`
	Kubeconfig    diagnosticsKubeconfig       `json:"kubeconfig"`
	Context       diagnosticsContext          `json:"context"`
	Discovery     kube.DiscoveryCacheSnapshot `json:"discovery"`
	Watches       diagnosticsWatches          `json:"watches"`
	RecentErrors  []diagnosticErrorEvent      `json:"recentErrors"`
	Sessions      diagnosticsSessions         `json:"sessions"`
	Audit         diagnosticsAudit            `json:"audit"`
	Requests      diagnosticsRequests         `json:"requests"`
}

// readinessResponse reports dependency readiness separately from local-process liveness.
type readinessResponse struct {
	Status     string                      `json:"status"`
	Ready      bool                        `json:"ready"`
	Kubeconfig diagnosticsKubeconfig       `json:"kubeconfig"`
	Context    diagnosticsContext          `json:"context"`
	Discovery  kube.DiscoveryCacheSnapshot `json:"discovery"`
	Audit      diagnosticsAudit            `json:"audit"`
}

// newDiagnosticsStore creates an empty bounded diagnostics store.
func newDiagnosticsStore() *diagnosticsStore {
	return &diagnosticsStore{
		startedAt:       time.Now().UTC(),
		statusCounts:    make(map[int]uint64),
		operationCounts: make(map[string]uint64),
		errorCounts:     make(map[string]uint64),
		sessions:        make(map[string]diagnosticSession),
		activeSessions:  make(map[string]uint64),
		watches:         make(map[string]diagnosticWatchStatus),
	}
}

// recordRequest records one request's safe metadata and bounded timing aggregates.
func (store *diagnosticsStore) recordRequest(telemetry requestTelemetry) {
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	status := telemetry.Status
	if status < 100 {
		status = http.StatusOK
	}
	category := telemetry.ErrorCategory
	if category == "" {
		category = errorCategoryForStatus(status)
	}
	store.requestTotal++
	store.durationTotal += telemetry.Duration
	if telemetry.Duration > store.maxDuration {
		store.maxDuration = telemetry.Duration
	}
	store.statusCounts[status]++
	operation := normalizeDiagnosticOperation(telemetry.Operation)
	store.operationCounts[operation]++
	if category != "" {
		store.recordErrorLocked(diagnosticErrorEvent{
			At:        time.Now().UTC(),
			Status:    status,
			Category:  boundedDiagnosticText(category, maxDiagnosticText),
			Operation: operation,
		})
	}
}

// recordSafeError records a classified stream or lifecycle error without retaining its text.
func (store *diagnosticsStore) recordSafeError(category, operation string, status int) {
	if store == nil || category == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recordErrorLocked(diagnosticErrorEvent{
		At:        time.Now().UTC(),
		Status:    status,
		Category:  boundedDiagnosticText(category, maxDiagnosticText),
		Operation: normalizeDiagnosticOperation(operation),
	})
}

// recordErrorLocked appends one bounded error and updates its aggregate category count.
func (store *diagnosticsStore) recordErrorLocked(event diagnosticErrorEvent) {
	event.Category = normalizeDiagnosticCategory(event.Category)
	if event.Category == "" {
		return
	}
	store.errorTotal++
	store.errorCounts[event.Category]++
	store.recentErrors = append(store.recentErrors, event)
	if len(store.recentErrors) > maxDiagnosticErrors {
		store.recentErrors = store.recentErrors[len(store.recentErrors)-maxDiagnosticErrors:]
	}
}

// startSession records an active terminal, log, port-forward, or watch session.
func (store *diagnosticsStore) startSession(kind, contextName, resource string) string {
	if store == nil {
		return ""
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	kind = normalizeDiagnosticSessionKind(kind)
	store.nextSessionID++
	id := fmt.Sprintf("%s:%d", kind, store.nextSessionID)
	store.activeSessions[kind]++
	if len(store.sessions) < maxDiagnosticSessions {
		store.sessions[id] = diagnosticSession{Kind: kind, Context: contextName, Resource: resource}
	}
	return id
}

// finishSession removes an active session and records an optional classified failure.
func (store *diagnosticsStore) finishSession(id, category, operation string) {
	if store == nil || id == "" {
		return
	}
	store.mu.Lock()
	kind := ""
	if session, found := store.sessions[id]; found {
		kind = session.Kind
		delete(store.sessions, id)
	}
	if kind == "" {
		kind = strings.SplitN(id, ":", 2)[0]
	}
	if count := store.activeSessions[kind]; count > 0 {
		store.activeSessions[kind] = count - 1
	}
	if category != "" {
		store.recordErrorLocked(diagnosticErrorEvent{
			At:        time.Now().UTC(),
			Category:  boundedDiagnosticText(category, maxDiagnosticText),
			Operation: normalizeDiagnosticOperation(operation),
		})
	}
	store.mu.Unlock()
}

// startWatch records a watch session and its safe opened or reconnected transition.
func (store *diagnosticsStore) startWatch(contextName, resource, operation string) diagnosticWatchHandle {
	if store == nil {
		return diagnosticWatchHandle{}
	}
	sessionID := store.startSession("watch", contextName, resource)
	key := contextName + "\x00" + resource
	store.mu.Lock()
	status, found := store.watches[key]
	tracked := found || len(store.watches) < maxDiagnosticWatches
	reconnect := found && !status.Active && status.State != ""
	status.Context = contextName
	status.Resource = resource
	status.State = "active"
	status.LastAt = time.Now().UTC()
	status.Error = ""
	status.Active = true
	if reconnect {
		status.Reconnects++
	}
	if tracked {
		store.watches[key] = status
	}
	store.retainWatchEventLocked(diagnosticWatchEvent{
		At:        status.LastAt,
		Context:   contextName,
		Resource:  resource,
		State:     "opened",
		Reconnect: reconnect,
	})
	store.mu.Unlock()
	return diagnosticWatchHandle{SessionID: sessionID, Key: key, Context: contextName, Resource: resource, Tracked: tracked}
}

// finishWatch records a safe watch close or error transition and removes its active session.
func (store *diagnosticsStore) finishWatch(handle diagnosticWatchHandle, state, category, operation string) {
	if store == nil || handle.SessionID == "" {
		return
	}
	store.finishSession(handle.SessionID, category, operation)
	store.mu.Lock()
	if !handle.Tracked {
		store.retainWatchEventLocked(diagnosticWatchEvent{
			At:       time.Now().UTC(),
			Context:  handle.Context,
			Resource: handle.Resource,
			State:    state,
			Error:    category,
		})
		store.mu.Unlock()
		return
	}
	status := store.watches[handle.Key]
	status.Context = handle.Context
	status.Resource = handle.Resource
	status.State = state
	status.LastAt = time.Now().UTC()
	status.Error = normalizeDiagnosticCategory(category)
	status.Active = false
	store.watches[handle.Key] = status
	store.retainWatchEventLocked(diagnosticWatchEvent{
		At:       status.LastAt,
		Context:  handle.Context,
		Resource: handle.Resource,
		State:    state,
		Error:    normalizeDiagnosticCategory(category),
	})
	store.mu.Unlock()
}

// retainWatchEventLocked appends one lifecycle event within the fixed diagnostics bound.
func (store *diagnosticsStore) retainWatchEventLocked(event diagnosticWatchEvent) {
	event.Error = normalizeDiagnosticCategory(event.Error)
	store.recentWatches = append(store.recentWatches, event)
	if len(store.recentWatches) > maxDiagnosticWatchEvents {
		store.recentWatches = store.recentWatches[len(store.recentWatches)-maxDiagnosticWatchEvents:]
	}
	if len(store.watches) <= maxDiagnosticWatches {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, status := range store.watches {
		if status.Active {
			continue
		}
		if oldestKey == "" || status.LastAt.Before(oldest) {
			oldestKey = key
			oldest = status.LastAt
		}
	}
	if oldestKey != "" {
		delete(store.watches, oldestKey)
	}
}

// snapshot builds the safe diagnostics response from current dependency metadata.
func (store *diagnosticsStore) snapshot(kubernetes kube.DiagnosticsSnapshot, auditState string, now time.Time) diagnosticsResponse {
	if store == nil {
		store = newDiagnosticsStore()
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	active := diagnosticsSessions{
		Terminal:    store.activeSessions["terminal"],
		Logs:        store.activeSessions["logs"],
		PortForward: store.activeSessions["port-forward"],
		Watch:       store.activeSessions["watch"],
	}
	statuses := make(map[string]uint64, len(store.statusCounts))
	for status, count := range store.statusCounts {
		statuses[fmt.Sprintf("%d", status)] = count
	}
	operations := make(map[string]uint64, len(store.operationCounts))
	for operation, count := range store.operationCounts {
		operations[operation] = count
	}
	errorCounts := make(map[string]uint64, len(store.errorCounts))
	for category, count := range store.errorCounts {
		errorCounts[category] = count
	}
	currentWatches := make([]diagnosticWatchView, 0, len(store.watches))
	for _, status := range store.watches {
		currentWatches = append(currentWatches, diagnosticWatchView{
			Context:    status.Context,
			Resource:   status.Resource,
			State:      status.State,
			LastAt:     status.LastAt,
			Error:      status.Error,
			Reconnects: status.Reconnects,
			Active:     status.Active,
		})
	}
	sort.Slice(currentWatches, func(left, right int) bool {
		return currentWatches[left].LastAt.After(currentWatches[right].LastAt)
	})
	if len(currentWatches) > maxDiagnosticWatches {
		currentWatches = currentWatches[:maxDiagnosticWatches]
	}
	recentErrors := append([]diagnosticErrorEvent{}, store.recentErrors...)
	recentWatches := append([]diagnosticWatchEvent{}, store.recentWatches...)
	averageMilliseconds := float64(0)
	if store.requestTotal > 0 {
		averageMilliseconds = float64(store.durationTotal) / float64(store.requestTotal) / float64(time.Millisecond)
	}
	return diagnosticsResponse{
		GeneratedAt:   now,
		UptimeSeconds: maxInt64(0, int64(now.Sub(store.startedAt)/time.Second)),
		Kubeconfig: diagnosticsKubeconfig{
			Source:   boundedDiagnosticText(kubernetes.KubeconfigSource, maxDiagnosticText),
			State:    boundedDiagnosticText(kubernetes.KubeconfigState, maxDiagnosticText),
			Revision: kubernetes.KubeconfigRevision,
		},
		Context: diagnosticsContext{
			Current:  boundedDiagnosticText(kubernetes.CurrentContext, maxDiagnosticText),
			Selected: boundedDiagnosticText(kubernetes.SelectedContext, maxDiagnosticText),
			State:    boundedDiagnosticText(kubernetes.ContextState, maxDiagnosticText),
		},
		Discovery: kubernetes.Discovery,
		Watches: diagnosticsWatches{
			Active:  active.Watch,
			Current: currentWatches,
			Recent:  recentWatches,
		},
		RecentErrors: recentErrors,
		Sessions:     active,
		Audit:        diagnosticsAudit{State: auditState},
		Requests: diagnosticsRequests{
			Total:                       store.requestTotal,
			Errors:                      store.errorTotal,
			AverageDurationMilliseconds: averageMilliseconds,
			MaxDurationMilliseconds:     float64(store.maxDuration) / float64(time.Millisecond),
			Statuses:                    statuses,
			Operations:                  operations,
			ErrorCategories:             errorCounts,
		},
	}
}

// handleDiagnostics returns bounded local diagnostics without dependency error text or credentials.
func (s *Server) handleDiagnostics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	kubernetes := kube.DiagnosticsSnapshot{KubeconfigState: "unavailable", ContextState: "unavailable", Discovery: kube.DiscoveryCacheSnapshot{State: "unavailable"}}
	if s.kube != nil {
		kubernetes = s.kube.Diagnostics(request.URL.Query().Get("context"))
	}
	auditState := s.auditState(request.Context())
	writeJSON(writer, http.StatusOK, s.diagnosticStore().snapshot(kubernetes, auditState, time.Now().UTC()))
}

// handleReadiness reports dependency readiness independently from process liveness.
func (s *Server) handleReadiness(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	result := s.readinessSnapshot(request.Context(), request.URL.Query().Get("context"))
	status := http.StatusOK
	if !result.Ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(writer, status, result)
}

// readinessSnapshot checks kubeconfig, selected context, discovery, and audit dependencies safely.
func (s *Server) readinessSnapshot(ctx context.Context, contextName string) readinessResponse {
	result := readinessResponse{
		Status:    "unavailable",
		Ready:     false,
		Audit:     diagnosticsAudit{State: "unavailable"},
		Discovery: kube.DiscoveryCacheSnapshot{State: "unavailable"},
	}
	if s.kube == nil {
		result.Kubeconfig = diagnosticsKubeconfig{State: "unavailable"}
		result.Context = diagnosticsContext{State: "unavailable"}
		return result
	}
	kubernetes := s.kube.Diagnostics(contextName)
	if kubernetes.ContextState == "available" && kubernetes.Discovery.State == "not-checked" {
		discoveryContext, cancel := context.WithTimeout(ctx, readinessDiscoveryTimeout)
		_, discoveryErr := s.kube.ResourceCatalog(discoveryContext, kubernetes.SelectedContext)
		cancel()
		if discoveryErr != nil {
			kubernetes.Discovery = s.kube.DiscoveryCacheStatus(kubernetes.SelectedContext)
			if kubernetes.Discovery.State == "not-checked" || kubernetes.Discovery.State == "loading" {
				kubernetes.Discovery.State = "unavailable"
			}
		} else {
			kubernetes = s.kube.Diagnostics(kubernetes.SelectedContext)
		}
	}
	auditState := s.auditState(ctx)
	result.Kubeconfig = diagnosticsKubeconfig{Source: kubernetes.KubeconfigSource, State: kubernetes.KubeconfigState, Revision: kubernetes.KubeconfigRevision}
	result.Context = diagnosticsContext{Current: kubernetes.CurrentContext, Selected: kubernetes.SelectedContext, State: kubernetes.ContextState}
	result.Discovery = kubernetes.Discovery
	result.Audit = diagnosticsAudit{State: auditState}
	result.Ready = kubernetes.KubeconfigState != "error" && kubernetes.ContextState == "available" && kubernetes.Discovery.State == "available" && auditState == "available"
	if result.Ready {
		result.Status = "ready"
	} else if kubernetes.ContextState == "available" || kubernetes.KubeconfigRevision > 0 {
		result.Status = "degraded"
	}
	return result
}

// auditState checks the local SQLite store without returning database error details.
func (s *Server) auditState(ctx context.Context) string {
	if s.audit == nil {
		return "unavailable"
	}
	checkContext, cancel := context.WithTimeout(ctx, readinessAuditTimeout)
	defer cancel()
	if err := s.audit.Ping(checkContext); err != nil {
		return "unavailable"
	}
	return "available"
}

// diagnosticStore returns the lazily initialized per-server diagnostics store.
func (s *Server) diagnosticStore() *diagnosticsStore {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	if s.diagnostics == nil {
		s.diagnostics = newDiagnosticsStore()
	}
	return s.diagnostics
}

// requestIDFor returns the middleware ID or creates a safe ID for direct handler use.
func requestIDFor(request *http.Request) string {
	if request != nil {
		if value, ok := request.Context().Value(requestIDContextKey{}).(string); ok && validRequestID(value) {
			return value
		}
		if validRequestID(request.Header.Get("X-Request-ID")) {
			return request.Header.Get("X-Request-ID")
		}
	}
	return newRequestID()
}

// newRequestID creates a cryptographically random, header-safe correlation ID.
func newRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err == nil {
		return hex.EncodeToString(value)
	}
	sequence := requestIDFallbackSequence.Add(1)
	return fmt.Sprintf("%x-%x", time.Now().UTC().UnixNano(), sequence)
}

// validRequestID accepts only bounded printable correlation IDs with no separator ambiguity.
func validRequestID(value string) bool {
	if value == "" || len(value) > maxRequestIDLength {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("._:-", character) {
			return false
		}
	}
	return true
}

// boundedDiagnosticText removes control characters and bounds safe operator-facing coordinates.
func boundedDiagnosticText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if maximum < 1 {
		return ""
	}
	result := make([]rune, 0, minInt(maximum, len(value)))
	for _, character := range value {
		if unicode.IsControl(character) {
			continue
		}
		result = append(result, character)
		if len(result) >= maximum {
			break
		}
	}
	return string(result)
}

// requestTelemetryFor extracts safe method, context, GVR, and operation metadata without reading the body.
func requestTelemetryFor(request *http.Request) requestTelemetry {
	return requestTelemetry{
		RequestID: requestIDFor(request),
		Method:    safeRequestMethod(request.Method),
		Context:   boundedDiagnosticText(request.URL.Query().Get("context"), maxDiagnosticText),
		Resource:  requestResourceIdentity(request),
		Operation: requestOperationClass(request.Method, request.URL.Path),
	}
}

// safeRequestMethod bounds unexpected HTTP methods before they reach logs.
func safeRequestMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect:
		return method
	default:
		return "OTHER"
	}
}

// requestResourceIdentity returns complete safe GVR or subresource identity when the route provides it.
func requestResourceIdentity(request *http.Request) string {
	query := request.URL.Query()
	group := boundedDiagnosticText(query.Get("group"), maxDiagnosticText)
	version := boundedDiagnosticText(query.Get("version"), maxDiagnosticText)
	resource := boundedDiagnosticText(query.Get("resource"), maxDiagnosticText)
	if version != "" && resource != "" {
		if group == "" {
			group = "core"
		}
		return group + "/" + version + "/" + resource
	}
	switch request.URL.Path {
	case "/api/events", "/api/events/watch":
		return "core/v1/events"
	case "/api/logs":
		return "core/v1/pods/log"
	case "/api/terminal":
		mode := query.Get("mode")
		if mode != "exec" && mode != "attach" {
			mode = "session"
		}
		return "core/v1/pods/" + mode
	case "/api/port-forward":
		return "core/v1/pods/portforward"
	case "/api/pod/debug":
		return "core/v1/pods/ephemeralcontainers"
	case "/api/pod/copy":
		return "core/v1/pods/exec"
	default:
		return ""
	}
}

// requestOperationClass maps routes to a stable bounded operation class for telemetry.
func requestOperationClass(method, path string) string {
	if path == "/api/health" || path == "/api/readiness" {
		return "health"
	}
	if path == "/api/diagnostics" {
		return "diagnostics"
	}
	if path == "/api/contexts/reload" {
		return "kubeconfig-reload"
	}
	if path == "/api/contexts" {
		return "kubeconfig-read"
	}
	if path == "/api/discovery" {
		return "discovery"
	}
	if path == "/api/watch" || path == "/api/events/watch" {
		return "watch"
	}
	if path == "/api/logs" {
		return "logs"
	}
	if path == "/api/terminal" {
		return "terminal"
	}
	if path == "/api/port-forward" {
		return "port-forward"
	}
	if path == "/api/audit" {
		return "audit-read"
	}
	if path == "/api/resources/delete" {
		return "bulk-delete"
	}
	if path == "/api/resource" {
		switch method {
		case http.MethodGet:
			return "resource-read"
		case http.MethodDelete:
			return "resource-delete"
		default:
			return "resource-apply"
		}
	}
	if path == "/api/resources" {
		return "resource-list"
	}
	if strings.HasPrefix(path, "/api/") {
		return "api"
	}
	return "frontend"
}

// errorCategoryForStatus converts an HTTP status into a safe stable category.
func errorCategoryForStatus(status int) string {
	switch {
	case status >= 200 && status < 400:
		return ""
	case status == http.StatusBadRequest:
		return "validation"
	case status == http.StatusForbidden || status == http.StatusUnauthorized:
		return "permission-denied"
	case status == http.StatusNotFound:
		return "not-found"
	case status == http.StatusConflict:
		return "conflict"
	case status == http.StatusNotImplemented:
		return "unsupported"
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return "timeout"
	case status == http.StatusTooManyRequests:
		return "rate-limited"
	case status >= 500:
		return "dependency-unavailable"
	default:
		return "request-failed"
	}
}

// retryClassification returns a stable browser hint without exposing backend diagnostics.
func retryClassification(status int, category string) string {
	switch category {
	case "validation", "permission-denied", "not-found", "unsupported":
		return "check-and-retry"
	case "conflict":
		return "refresh-and-review"
	case "timeout", "rate-limited", "dependency-unavailable", "network", "stream-error", "watch-error", "watch-closed":
		return "retry"
	default:
		if status >= 500 {
			return "retry"
		}
		return "check-and-retry"
	}
}

// safeErrorCategory classifies an internal or Kubernetes error without retaining its message.
func safeErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
		return "timeout"
	}
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return "permission-denied"
	case apierrors.IsConflict(err):
		return "conflict"
	case apierrors.IsNotFound(err):
		return "not-found"
	case apierrors.IsTooManyRequests(err):
		return "rate-limited"
	case apierrors.IsMethodNotSupported(err):
		return "unsupported"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "permission"), strings.Contains(message, "forbidden"), strings.Contains(message, "unauthorized"), strings.Contains(message, "rbac"):
		return "permission-denied"
	case strings.Contains(message, "conflict"), strings.Contains(message, "resource version"):
		return "conflict"
	case strings.Contains(message, "not supported"), strings.Contains(message, "unsupported"):
		return "unsupported"
	case strings.Contains(message, "load kubeconfig"):
		return "dependency-unavailable"
	case strings.Contains(message, "required"), strings.Contains(message, "invalid"), strings.Contains(message, "parse"), strings.Contains(message, "decode"):
		return "validation"
	case strings.Contains(message, "network"), strings.Contains(message, "connection"), strings.Contains(message, "transport"):
		return "network"
	default:
		return "dependency-unavailable"
	}
}

// normalizeDiagnosticCategory restricts retained categories to the fixed safe vocabulary.
func normalizeDiagnosticCategory(category string) string {
	if category == "" {
		return ""
	}
	switch category {
	case "canceled", "conflict", "dependency-unavailable", "network", "not-found", "permission-denied", "rate-limited", "request-failed", "stream-error", "timeout", "unsupported", "validation", "watch-closed", "watch-error", "resource-version-expired":
		return category
	default:
		return "request-failed"
	}
}

// normalizeDiagnosticOperation restricts retained operations to the fixed telemetry vocabulary.
func normalizeDiagnosticOperation(operation string) string {
	switch operation {
	case "api", "audit-read", "bulk-delete", "diagnostics", "discovery", "frontend", "health", "kubeconfig-read", "kubeconfig-reload", "logs", "port-forward", "resource-apply", "resource-delete", "resource-list", "resource-read", "terminal", "watch":
		return operation
	default:
		return "api"
	}
}

// normalizeDiagnosticSessionKind restricts active session counters to known stream classes.
func normalizeDiagnosticSessionKind(kind string) string {
	switch kind {
	case "logs", "port-forward", "terminal", "watch":
		return kind
	default:
		return "other"
	}
}

// safeErrorResponseMessage keeps local validation and conflict guidance while masking raw Kubernetes status text.
func safeErrorResponseMessage(err error, status int) string {
	if err == nil {
		return "request failed"
	}
	if status == http.StatusNotImplemented {
		return "This operation is not available on the Kubernetes API server"
	}
	message := err.Error()
	lowerMessage := strings.ToLower(message)
	category := safeErrorCategory(err)
	switch category {
	case "permission-denied":
		return "The operation was denied by Kubernetes RBAC"
	case "conflict":
		return "The operation conflicted with a newer resource version; refresh and retry"
	case "not-found":
		return "The requested Kubernetes resource was not found; refresh and retry"
	case "timeout":
		return "The Kubernetes API did not respond in time; retry the request"
	case "unsupported":
		return "This operation is not available on the Kubernetes API server"
	}
	if strings.Contains(lowerMessage, "secret") || strings.Contains(lowerMessage, "token") || strings.Contains(lowerMessage, "credential") || strings.Contains(lowerMessage, "authorization") || strings.Contains(lowerMessage, "private") {
		return "The request failed without exposing sensitive backend details"
	}
	var statusError apierrors.APIStatus
	if errors.As(err, &statusError) {
		switch category {
		case "permission-denied":
			return "Kubernetes denied this request; check RBAC permissions for the selected operation"
		case "conflict":
			return "Kubernetes reported a resource conflict; refresh and review before retrying"
		case "not-found":
			return "The requested Kubernetes resource was not found; refresh before retrying"
		case "unsupported":
			return "The Kubernetes API does not support this operation"
		case "timeout":
			return "The Kubernetes API did not respond in time; retry the request"
		default:
			return "The Kubernetes API request failed; retry after checking cluster connectivity"
		}
	}
	if status >= 500 {
		switch message {
		case "metrics collection transport failed", "relationship transport failed", "schema transport failed":
			return message
		default:
			return "The local backend dependency is unavailable; retry the request"
		}
	}
	return boundedDiagnosticText(message, 512)
}

// safeStreamErrorMessage returns useful local validation while masking native stream failures.
func safeStreamErrorMessage(err error) string {
	if err == nil {
		return "The native Kubernetes stream closed."
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "namespace is required") || strings.Contains(message, "pod name is required") || strings.Contains(message, "port must be between") || strings.Contains(message, "command is required") {
		return boundedDiagnosticText(err.Error(), 256)
	}
	switch safeErrorCategory(err) {
	case "permission-denied":
		return "Kubernetes denied this native stream; check RBAC permissions and retry."
	case "timeout":
		return "The native Kubernetes stream timed out; retry the session."
	case "unsupported":
		return "This native stream is not supported by the Kubernetes API."
	default:
		return "The native Kubernetes stream failed; retry the session."
	}
}

// markResponseError records a classified error when the response is managed by request middleware.
func markResponseError(writer http.ResponseWriter, category string) {
	if recorder, ok := writer.(interface{ SetErrorCategory(string) }); ok {
		recorder.SetErrorCategory(category)
	}
}

// safeErrorFields returns stable error metadata without including backend error text.
func safeErrorFields(writer http.ResponseWriter, err error, status int) map[string]string {
	category := safeErrorCategory(err)
	if category == "" {
		category = errorCategoryForStatus(status)
	}
	return safeErrorFieldsForCategory(writer, category, status)
}

// safeErrorFieldsForCategory builds request ID, category, and retry fields for a safe response.
func safeErrorFieldsForCategory(writer http.ResponseWriter, category string, status int) map[string]string {
	category = normalizeDiagnosticCategory(category)
	if category == "" {
		category = errorCategoryForStatus(status)
	}
	markResponseError(writer, category)
	fields := map[string]string{
		"errorCategory": category,
		"retry":         retryClassification(status, category),
	}
	if requestID := responseRequestID(writer); requestID != "" {
		fields["requestId"] = requestID
	} else {
		requestID := newRequestID()
		writer.Header().Set("X-Request-ID", requestID)
		fields["requestId"] = requestID
	}
	return fields
}

// responseRequestID returns a middleware correlation ID when the writer exposes one.
func responseRequestID(writer http.ResponseWriter) string {
	if recorder, ok := writer.(interface{ RequestID() string }); ok {
		return recorder.RequestID()
	}
	return ""
}

// maxInt64 returns the larger value and keeps uptime calculations non-negative.
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

// minInt returns the smaller integer for bounded rune capacity calculations.
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
