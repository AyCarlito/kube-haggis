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

// TestHandleRelatedReturnsSanitizedRelationshipPayload verifies the read-only relationship endpoint contract.
func TestHandleRelatedReturnsSanitizedRelationshipPayload(t *testing.T) {
	called := false
	server := &Server{
		related: func(_ context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (kube.RelatedResourceList, error) {
			called = true
			if contextName != "field" || group != "apps" || version != "v1" || resource != "deployments" || namespace != "default" || name != "api" || !namespaced {
				t.Fatalf("unexpected relationship coordinates: %q %q %q %q %q %q %t", contextName, group, version, resource, namespace, name, namespaced)
			}
			return kube.RelatedResourceList{
				State:   "partial",
				Items:   []kube.RelatedResource{{Type: "owner", Source: "metadata.ownerReferences", State: "resolved", Name: "api-controller"}},
				Notices: []kube.RelatedRelationshipNotice{{Type: "owned-descendant", Source: "metadata.ownerReferences", State: "permission-denied", Message: "RBAC denied"}},
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/related?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true", nil)
	response := httptest.NewRecorder()

	server.handleRelated(response, request)

	if response.Code != http.StatusOK || !called {
		t.Fatalf("relationship endpoint failed: %d %s", response.Code, response.Body.String())
	}
	var payload kube.RelatedResourceList
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode relationship response: %v", err)
	}
	if payload.State != "partial" || len(payload.Items) != 1 || len(payload.Notices) != 1 || payload.Notices[0].State != "permission-denied" {
		t.Fatalf("relationship payload was not preserved: %+v", payload)
	}
}

// TestHandleRelatedReturnsBackendError verifies relationship aggregation errors use the standard API response.
func TestHandleRelatedReturnsBackendError(t *testing.T) {
	server := &Server{related: func(context.Context, string, string, string, string, string, string, bool) (kube.RelatedResourceList, error) {
		return kube.RelatedResourceList{}, errString("relationship transport failed")
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/related", nil)
	response := httptest.NewRecorder()

	server.handleRelated(response, request)

	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "relationship transport failed") {
		t.Fatalf("expected relationship backend error, got %d %s", response.Code, response.Body.String())
	}
}
