package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kube-haggis/kube-haggis/internal/kube"
)

// TestHandleSchemaReturnsSafeStructuralSchema verifies schema coordinates and normalized fields reach the browser API.
func TestHandleSchemaReturnsSafeStructuralSchema(t *testing.T) {
	called := false
	server := &Server{
		resourceSchema: func(_ context.Context, contextName, group, version, resource string) (kube.ResourceSchema, error) {
			called = true
			if contextName != "field" || group != "widgets.example.com" || version != "v1" || resource != "widgets" {
				t.Fatalf("unexpected schema coordinates: %q %q %q %q", contextName, group, version, resource)
			}
			return kube.ResourceSchema{
				State:    "available",
				Group:    group,
				Version:  version,
				Resource: resource,
				Kind:     "Widget",
				Schema:   &kube.SchemaField{Type: "object", Properties: []kube.SchemaField{{Name: "spec", Type: "object", Required: true}}},
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/schema?context=field&group=widgets.example.com&version=v1&resource=widgets", nil)
	response := httptest.NewRecorder()

	server.handleSchema(response, request)

	if response.Code != http.StatusOK || !called {
		t.Fatalf("schema endpoint failed: status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}
	var payload kube.ResourceSchema
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode schema response: %v", err)
	}
	if payload.State != "available" || payload.Kind != "Widget" || payload.Schema == nil || len(payload.Schema.Properties) != 1 || !payload.Schema.Properties[0].Required {
		t.Fatalf("schema payload was not preserved: %+v", payload)
	}
}

// TestHandleSchemaPreservesExplicitFallbackStates verifies missing and unsupported schemas remain successful, actionable responses.
func TestHandleSchemaPreservesExplicitFallbackStates(t *testing.T) {
	server := &Server{resourceSchema: func(context.Context, string, string, string, string) (kube.ResourceSchema, error) {
		return kube.ResourceSchema{State: "non-structural", Message: "use YAML editor"}, nil
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	response := httptest.NewRecorder()

	server.handleSchema(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "non-structural") || !strings.Contains(response.Body.String(), "use YAML editor") {
		t.Fatalf("fallback state was not explicit: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestHandleSchemaReturnsBackendError verifies unexpected schema loader failures use the standard API error response.
func TestHandleSchemaReturnsBackendError(t *testing.T) {
	server := &Server{resourceSchema: func(context.Context, string, string, string, string) (kube.ResourceSchema, error) {
		return kube.ResourceSchema{}, errString("schema transport failed")
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/schema", nil)
	response := httptest.NewRecorder()

	server.handleSchema(response, request)

	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "schema transport failed") {
		t.Fatalf("schema backend error was not returned: status=%d body=%s", response.Code, response.Body.String())
	}
}
