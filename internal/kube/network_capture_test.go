package kube

import (
	"errors"
	"strings"
	"testing"
)

// TestNetworkCaptureContainerStartingRecognizesRetryableExecStates verifies startup retries do not hide permanent failures.
func TestNetworkCaptureContainerStartingRecognizesRetryableExecStates(t *testing.T) {
	if !networkCaptureContainerStarting(errors.New("container is not running")) {
		t.Fatal("not-running exec failure was not retryable")
	}
	if !networkCaptureContainerStarting(errors.New("container is waiting to start")) {
		t.Fatal("waiting exec failure was not retryable")
	}
	if networkCaptureContainerStarting(errors.New("forbidden")) {
		t.Fatal("permission failure was incorrectly marked retryable")
	}
}

// TestNetworkDebugContainerWaitingFailureExplainsRunAsNonRoot verifies kubelet config errors become actionable guidance.
func TestNetworkDebugContainerWaitingFailureExplainsRunAsNonRoot(t *testing.T) {
	err := networkDebugContainerWaitingFailure("CreateContainerConfigError", "container has runAsNonRoot and image has non-numeric user (root)")
	if err == nil || !strings.Contains(err.Error(), "non-root-compatible network debug image") {
		t.Fatalf("runAsNonRoot guidance was not preserved: %v", err)
	}
	if networkDebugContainerWaitingFailure("ContainerCreating", "") != nil {
		t.Fatal("normal container startup was classified as a failure")
	}
}
