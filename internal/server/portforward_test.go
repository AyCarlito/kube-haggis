package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParsePortForwardRequestAcceptsValidCoordinates verifies the browser protocol query shape.
func TestParsePortForwardRequestAcceptsValidCoordinates(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/port-forward?context=field&namespace=default&pod=api&targetPort=8080&localPort=18080", nil)
	parsed, err := parsePortForwardRequest(request)
	if err != nil {
		t.Fatalf("valid port-forward request rejected: %v", err)
	}
	if parsed.contextName != "field" || parsed.namespace != "default" || parsed.pod != "api" || parsed.targetPort != 8080 || parsed.localPort != 18080 {
		t.Fatalf("unexpected parsed request: %+v", parsed)
	}
}

// TestParsePortForwardRequestRejectsInvalidCoordinates verifies required and bounded fields.
func TestParsePortForwardRequestRejectsInvalidCoordinates(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "missing namespace", query: "pod=api&targetPort=8080&localPort=18080", want: "namespace is required"},
		{name: "missing Pod", query: "namespace=default&targetPort=8080&localPort=18080", want: "Pod name is required"},
		{name: "missing target", query: "namespace=default&pod=api&localPort=18080", want: "target port is required"},
		{name: "invalid target", query: "namespace=default&pod=api&targetPort=65536&localPort=18080", want: "target port must be between"},
		{name: "invalid local", query: "namespace=default&pod=api&targetPort=8080&localPort=0", want: "local port must be between"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/port-forward?"+test.query, nil)
			_, err := parsePortForwardRequest(request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// TestValidatePortForwardOriginRestrictsBrowserSessions verifies the local-only WebSocket policy.
func TestValidatePortForwardOriginRestrictsBrowserSessions(t *testing.T) {
	local := httptest.NewRequest("GET", "/api/port-forward", nil)
	local.Header.Set("Origin", "http://127.0.0.1:5173")
	if err := validatePortForwardOrigin(local); err != nil {
		t.Fatalf("local origin rejected: %v", err)
	}

	remote := httptest.NewRequest("GET", "/api/port-forward", nil)
	remote.Header.Set("Origin", "https://outside.example")
	if err := validatePortForwardOrigin(remote); err == nil || !strings.Contains(err.Error(), "must be local") {
		t.Fatalf("remote origin was accepted: %v", err)
	}
}
