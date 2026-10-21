package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AyCarlito/kube-haggis/internal/kube"
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

// TestHandleRelatedReverseUsesDeferredLoader verifies the secondary reverse-owner endpoint preserves safe results.
func TestHandleRelatedReverseUsesDeferredLoader(t *testing.T) {
	called := false
	server := &Server{
		relatedReverse: func(_ context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (kube.RelatedResourceList, error) {
			called = true
			if contextName != "field" || group != "apps" || version != "v1" || resource != "deployments" || namespace != "default" || name != "api" || !namespaced {
				t.Fatalf("unexpected reverse relationship coordinates: %q %q %q %q %q %q %t", contextName, group, version, resource, namespace, name, namespaced)
			}
			return kube.RelatedResourceList{State: "available", Items: []kube.RelatedResource{}}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/related/reverse?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true", nil)
	response := httptest.NewRecorder()

	server.handleRelatedReverse(response, request)

	if response.Code != http.StatusOK || !called {
		t.Fatalf("deferred relationship endpoint failed: %d %s", response.Code, response.Body.String())
	}
}

// TestHandleRelatedReverseDoesNotLoadEvents verifies the reverse endpoint cannot be used to scan Event descendants.
func TestHandleRelatedReverseDoesNotLoadEvents(t *testing.T) {
	called := false
	server := &Server{
		relatedReverse: func(context.Context, string, string, string, string, string, string, bool) (kube.RelatedResourceList, error) {
			called = true
			return kube.RelatedResourceList{}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/related/reverse?context=field&group=&version=v1&resource=events&namespace=default&name=warning&namespaced=true", nil)
	response := httptest.NewRecorder()

	server.handleRelatedReverse(response, request)

	if response.Code != http.StatusOK || called {
		t.Fatalf("Event reverse relationship loader was invoked: %d %s", response.Code, response.Body.String())
	}
	var payload kube.RelatedResourceList
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode Event reverse response: %v", err)
	}
	if payload.State != "empty" || len(payload.Items) != 0 || !strings.Contains(payload.Message, "involvedObject") {
		t.Fatalf("Event reverse response did not explain omission: %+v", payload)
	}
}
