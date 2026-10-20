package kube

import (
	"context"
	"strings"
	"testing"
)

// TestValidatePodPortForwardRejectsIncompleteCoordinates verifies the native session guardrails.
func TestValidatePodPortForwardRejectsIncompleteCoordinates(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		pod        string
		targetPort int
		localPort  int
		want       string
	}{
		{name: "missing namespace", pod: "api", targetPort: 8080, localPort: 18080, want: "namespace is required"},
		{name: "all namespaces", namespace: "all", pod: "api", targetPort: 8080, localPort: 18080, want: "namespace is required"},
		{name: "missing Pod", namespace: "default", targetPort: 8080, localPort: 18080, want: "Pod name is required"},
		{name: "invalid target port", namespace: "default", pod: "api", targetPort: 0, localPort: 18080, want: "target port must be between"},
		{name: "invalid local port", namespace: "default", pod: "api", targetPort: 8080, localPort: 65536, want: "local port must be between"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePodPortForward(test.namespace, test.pod, test.targetPort, test.localPort)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

// TestForwardPodPortValidatesBeforeClientCreation verifies malformed requests do not require a kubeconfig.
func TestForwardPodPortValidatesBeforeClientCreation(t *testing.T) {
	service := NewService("")
	err := service.ForwardPodPort(context.Background(), "", "default", "api", 0, 18080, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "target port must be between") {
		t.Fatalf("expected target port validation error, got %v", err)
	}
}
