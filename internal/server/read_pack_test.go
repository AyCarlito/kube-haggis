package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kube-haggis/kube-haggis/internal/kube"
)

// TestHandleOperationalReadPackPreservesSafePanelStates verifies the single read-pack response seam.
func TestHandleOperationalReadPackPreservesSafePanelStates(t *testing.T) {
	called := false
	server := &Server{
		readPack: func(_ context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (kube.OperationalReadPack, error) {
			called = true
			if contextName != "field" || group != "networking.k8s.io" || version != "v1" || resource != "networkpolicies" || namespace != "default" || name != "api" || !namespaced {
				t.Fatalf("unexpected read-pack coordinates: %q %q %q %q %q %q %t", contextName, group, version, resource, namespace, name, namespaced)
			}
			return kube.OperationalReadPack{
				State:   "partial",
				Message: "one optional family is unavailable",
				Panels:  []kube.OperationalReadPackPanel{{Key: "network-policy", Title: "Network policy coverage", State: "partial", Message: "Kubernetes RBAC denied affected Pod reads."}},
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/read-pack?context=field&group=networking.k8s.io&version=v1&resource=networkpolicies&namespace=default&name=api&namespaced=true", nil)
	response := httptest.NewRecorder()
	server.handleOperationalReadPack(response, request)
	if response.Code != http.StatusOK || !called || !strings.Contains(response.Body.String(), "RBAC") {
		t.Fatalf("read-pack handler did not preserve safe panel state: status=%d called=%t body=%s", response.Code, called, response.Body.String())
	}
}

// TestHandleOperationalReadPackRedactsLoaderErrors verifies raw Kubernetes errors never reach the browser.
func TestHandleOperationalReadPackRedactsLoaderErrors(t *testing.T) {
	server := &Server{
		readPack: func(context.Context, string, string, string, string, string, string, bool) (kube.OperationalReadPack, error) {
			return kube.OperationalReadPack{}, errString("Secret value private-value was rejected by the provider")
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/read-pack", nil)
	response := httptest.NewRecorder()
	server.handleOperationalReadPack(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "private-value") || strings.Contains(response.Body.String(), "provider") {
		t.Fatalf("read-pack handler exposed loader detail: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestHandleOperationalReadPackRejectsMutationMethods verifies the read pack has no mutation path.
func TestHandleOperationalReadPackRejectsMutationMethods(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/read-pack", nil)
	response := httptest.NewRecorder()
	server.handleOperationalReadPack(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("read-pack mutation method was not rejected: status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}
