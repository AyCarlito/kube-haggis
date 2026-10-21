package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/audit"
	"github.com/AyCarlito/kube-haggis/internal/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestHandleResourceDeleteRequiresConfirmation verifies final generic deletes cannot bypass review.
func TestHandleResourceDeleteRequiresConfirmation(t *testing.T) {
	called := false
	server := &Server{
		resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
			called = true
			return nil
		},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7", nil)
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleResource(response, request)

	if response.Code != http.StatusBadRequest || called || !strings.Contains(response.Body.String(), "confirmation") {
		t.Fatalf("delete bypassed confirmation: status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}
}

// TestHandleResourceDeleteDryRunUsesResourceVersion verifies generic deletion review reaches Kubernetes with a precondition.
func TestHandleResourceDeleteDryRunUsesResourceVersion(t *testing.T) {
	var received kube.ResourceDeleteOptions
	server := &Server{
		resourceDelete: func(_ context.Context, contextName, group, version, resource, namespace, name string, namespaced bool, options kube.ResourceDeleteOptions) error {
			if contextName != "field" || group != "apps" || version != "v1" || resource != "deployments" || namespace != "default" || name != "api" || !namespaced {
				t.Fatalf("unexpected delete coordinates: %q %q %q %q %q %q %t", contextName, group, version, resource, namespace, name, namespaced)
			}
			received = options
			return nil
		},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&dryRun=true", nil)
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleResource(response, request)

	if response.Code != http.StatusOK || !received.DryRun || received.ExpectedResourceVersion != "7" || !strings.Contains(response.Body.String(), "dry-run passed") {
		t.Fatalf("generic delete review was not preserved: status=%d options=%+v body=%s", response.Code, received, response.Body.String())
	}
	if response.Header().Get(mutationReviewHeader) == "" {
		t.Fatal("generic delete dry-run did not return a review handle")
	}
}

// TestHandleResourceDeleteFinalConfirmationUsesResourceVersion verifies the final request remains explicitly confirmed and versioned.
func TestHandleResourceDeleteFinalConfirmationUsesResourceVersion(t *testing.T) {
	var received kube.ResourceDeleteOptions
	server := &Server{
		resourceDelete: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ string, _ bool, options kube.ResourceDeleteOptions) error {
			received = options
			return nil
		},
	}
	reviewRequest := httptest.NewRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&dryRun=true", nil)
	reviewRequest.Header.Set("X-KubeHaggis-Write", "enabled")
	reviewResponse := httptest.NewRecorder()
	server.handleResource(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK || reviewResponse.Header().Get(mutationReviewHeader) == "" {
		t.Fatalf("generic delete review failed: status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&confirmed=true&confirmName=api", nil)
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	request.Header.Set(mutationReviewHeader, reviewResponse.Header().Get(mutationReviewHeader))
	response := httptest.NewRecorder()

	server.handleResource(response, request)

	if response.Code != http.StatusOK || received.DryRun || received.ExpectedResourceVersion != "7" || !strings.Contains(response.Body.String(), "deleted") {
		t.Fatalf("final delete confirmation was not dispatched safely: status=%d options=%+v body=%s", response.Code, received, response.Body.String())
	}
}

// TestHandleResourceDeleteRedactsFailure verifies delete errors and audit details never expose request data.
func TestHandleResourceDeleteRedactsFailure(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	server := &Server{
		audit: store,
		resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
			return errString("Secret value private-value was denied by Kubernetes RBAC")
		},
	}
	reviewRequest := httptest.NewRequest(http.MethodDelete, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&expectedResourceVersion=9&dryRun=true", nil)
	reviewRequest.Header.Set("X-KubeHaggis-Write", "enabled")
	reviewResponse := httptest.NewRecorder()
	server.resourceDelete = func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
		return nil
	}
	server.handleResource(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK || reviewResponse.Header().Get(mutationReviewHeader) == "" {
		t.Fatalf("secret delete review failed: status=%d body=%s", reviewResponse.Code, reviewResponse.Body.String())
	}
	server.resourceDelete = func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
		return errString("Secret value private-value was denied by Kubernetes RBAC")
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&expectedResourceVersion=9&confirmed=true&confirmName=credentials", nil)
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	request.Header.Set(mutationReviewHeader, reviewResponse.Header().Get(mutationReviewHeader))
	response := httptest.NewRecorder()

	server.handleResource(response, request)

	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private-value") {
		t.Fatalf("unsafe delete error response: status=%d body=%s", response.Code, response.Body.String())
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected delete audit entries: %v %+v", err, entries)
	}
	if entries[0].Success || entries[1].DryRun == false || !entries[1].Success || strings.Contains(entries[0].Detail, "private-value") || strings.Contains(entries[0].Detail, "Secret") {
		t.Fatalf("unsafe delete audit entries: %+v", entries)
	}
	if strings.Contains(entries[1].Detail, "private-value") || strings.Contains(entries[1].Detail, "Secret") {
		t.Fatalf("unsafe delete review audit entry: %+v", entries[1])
	}
}

// TestHandleBulkResourceDeleteReturnsPerItemResults verifies bounded bulk execution continues after one item fails.
func TestHandleBulkResourceDeleteReturnsPerItemResults(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	called := make([]string, 0, 2)
	server := &Server{
		audit: store,
		resourceDelete: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, name string, _ bool, options kube.ResourceDeleteOptions) error {
			called = append(called, name)
			if !options.DryRun {
				t.Fatalf("bulk review unexpectedly submitted a mutation")
			}
			if name == "bad" {
				return errString("Secret value private-value was denied by Kubernetes RBAC")
			}
			return nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/resources/delete?context=field&group=&version=v1&resource=secrets&namespace=default&namespaced=true", strings.NewReader(`{"dryRun":true,"items":[{"name":"good","namespace":"default","resourceVersion":"7"},{"name":"bad","namespace":"default","resourceVersion":"8"}]}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleBulkResourceDelete(response, request)

	var result kube.BulkDeleteResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode bulk delete response: %v", err)
	}
	if response.Code != http.StatusOK || len(called) != 2 || result.Total != 2 || result.Succeeded != 1 || result.Failed != 1 || result.Status != "partial" {
		t.Fatalf("unexpected bulk delete response: status=%d called=%v result=%+v", response.Code, called, result)
	}
	if len(result.Items) != 2 || !result.Items[0].Success || result.Items[1].Success || strings.Contains(result.Items[1].Error, "private-value") {
		t.Fatalf("unexpected per-item results: %+v", result.Items)
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("unexpected bulk delete audit entries: %v %+v", err, entries)
	}
	for _, entry := range entries {
		if entry.Operation != "bulk-delete" || strings.Contains(entry.Detail, "private-value") || strings.Contains(entry.Detail, "Secret") {
			t.Fatalf("unsafe bulk delete audit entry: %+v", entry)
		}
	}
}

// TestHandleBulkResourceDeleteRequiresStrongConfirmation verifies final bulk mutation is blocked without the exact phrase.
func TestHandleBulkResourceDeleteRequiresStrongConfirmation(t *testing.T) {
	called := false
	server := &Server{
		resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
			called = true
			return nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/resources/delete?context=field&group=apps&version=v1&resource=deployments&namespace=default&namespaced=true", strings.NewReader(`{"dryRun":false,"confirmed":true,"confirmation":"DELETE","items":[{"name":"api","namespace":"default","resourceVersion":"7"}]}`))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()

	server.handleBulkResourceDelete(response, request)

	if response.Code != http.StatusBadRequest || called || !strings.Contains(response.Body.String(), "confirmation") {
		t.Fatalf("bulk delete bypassed review semantics: status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}
}

// TestValidateBulkDeleteRequestRejectsUnsafeSelections verifies duplicate and cross-namespace selections are rejected before mutation.
func TestValidateBulkDeleteRequestRejectsUnsafeSelections(t *testing.T) {
	tests := []struct {
		name    string
		request kube.BulkDeleteRequest
		ns      string
		want    string
	}{
		{
			name: "duplicate",
			request: kube.BulkDeleteRequest{Items: []kube.ResourceDeleteItem{
				{Name: "api", Namespace: "default", ResourceVersion: "1"},
				{Name: "api", Namespace: "default", ResourceVersion: "2"},
			}},
			ns:   "default",
			want: "selected more than once",
		},
		{
			name: "namespace mismatch",
			request: kube.BulkDeleteRequest{Items: []kube.ResourceDeleteItem{
				{Name: "api", Namespace: "other", ResourceVersion: "1"},
			}},
			ns:   "default",
			want: "does not match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateBulkDeleteRequest(test.request, test.ns, true)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected validation error containing %q, got %v", test.want, err)
			}
		})
	}
}

// TestHandleResourceConflictRefreshesCurrentObjectSafely verifies the dedicated conflict payload and redactions.
func TestHandleResourceConflictRefreshesCurrentObjectSafely(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer store.Close()
	getCalled := false
	server := &Server{
		audit: store,
		resourceApply: func(context.Context, string, string, string, string, string, bool, bool, []byte) (*unstructured.Unstructured, error) {
			return nil, apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, "credentials", errString("Secret value private-value conflicted with a newer resource version"))
		},
		resourceGet: func(_ context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (*unstructured.Unstructured, error) {
			getCalled = true
			if contextName != "field" || group != "" || version != "v1" || resource != "secrets" || namespace != "default" || name != "credentials" || !namespaced {
				t.Fatalf("unexpected conflict refresh coordinates: %q %q %q %q %q %q %t", contextName, group, version, resource, namespace, name, namespaced)
			}
			return &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":            "credentials",
					"namespace":       "default",
					"resourceVersion": "12",
					"managedFields":   []interface{}{map[string]interface{}{"manager": "controller", "value": "managed-private-value"}},
				},
				"data":       map[string]interface{}{"password": "current-private-value"},
				"stringData": map[string]interface{}{"token": "current-token-value"},
			}}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&dryRun=true", strings.NewReader("apiVersion: v1\nkind: Secret\nmetadata:\n  name: credentials\n  namespace: default\n  resourceVersion: \"7\"\ndata:\n  password: local-private-value\n"))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	response := httptest.NewRecorder()
	server.handleResource(response, request)

	var payload kube.ResourceConflictResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if response.Code != http.StatusConflict || !payload.Conflict || !payload.CurrentAvailable || payload.CurrentResourceVersion != "12" || !getCalled {
		t.Fatalf("conflict response did not refresh current state: status=%d payload=%+v getCalled=%t", response.Code, payload, getCalled)
	}
	if strings.Contains(response.Body.String(), "private-value") || strings.Contains(response.Body.String(), "token-value") || strings.Contains(response.Body.String(), "managedFields") {
		t.Fatalf("conflict response exposed sensitive or server-owned data: %s", response.Body.String())
	}
	metadata, ok := payload.CurrentObject["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("conflict response omitted current metadata: %+v", payload.CurrentObject)
	}
	if _, found := metadata["managedFields"]; found {
		t.Fatal("conflict response retained managedFields")
	}
	data, ok := payload.CurrentObject["data"].(map[string]interface{})
	if !ok || data["password"] != "[redacted]" {
		t.Fatalf("conflict response did not redact Secret data: %+v", payload.CurrentObject["data"])
	}
	if payload.Summary.LocalResourceVersion != "7" || payload.Summary.CurrentResourceVersion != "12" {
		t.Fatalf("conflict summary omitted resource versions: %+v", payload.Summary)
	}
	entries, err := store.List(context.Background(), 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected conflict audit entries: %v %+v", err, entries)
	}
	if entries[0].Success || strings.Contains(entries[0].Detail, "private-value") || strings.Contains(entries[0].Detail, "Secret") {
		t.Fatalf("unsafe conflict audit entry: %+v", entries[0])
	}
}

// TestIsResourceVersionConflictClassifiesOnlyConflictSignals verifies generic update conflict detection.
func TestIsResourceVersionConflictClassifiesOnlyConflictSignals(t *testing.T) {
	conflict := apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api", errString("resourceVersion changed"))
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "kubernetes conflict", err: conflict, want: true},
		{name: "resource version text", err: errString("resource version is stale"), want: true},
		{name: "unrelated conflict text", err: errString("admission conflict"), want: false},
		{name: "validation failure", err: errString("resource document is invalid"), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isResourceVersionConflict(test.err); got != test.want {
				t.Fatalf("isResourceVersionConflict(%v)=%t, want %t", test.err, got, test.want)
			}
		})
	}
}
