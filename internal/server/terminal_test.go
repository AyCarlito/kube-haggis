package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTerminalCommandDefaultsToShell verifies the safe default exec command.
func TestTerminalCommandDefaultsToShell(t *testing.T) {
	command := terminalCommand("  ")
	if len(command) != 1 || command[0] != "/bin/sh" {
		t.Fatalf("unexpected default command: %#v", command)
	}
}

// TestTerminalCommandSplitsArguments verifies browser command input becomes argv values.
func TestTerminalCommandSplitsArguments(t *testing.T) {
	command := terminalCommand("/bin/sh -c 'printf hello'")
	want := []string{"/bin/sh", "-c", "'printf", "hello'"}
	if len(command) != len(want) {
		t.Fatalf("unexpected command length: %#v", command)
	}
	for index := range want {
		if command[index] != want[index] {
			t.Fatalf("unexpected command: %#v", command)
		}
	}
}

// TestValidateTerminalOrigin restricts terminal WebSockets to local browser origins.
func TestValidateTerminalOrigin(t *testing.T) {
	local := httptest.NewRequest("GET", "/api/terminal", nil)
	local.Header.Set("Origin", "http://localhost:5173")
	if err := validateTerminalOrigin(local); err != nil {
		t.Fatalf("local origin rejected: %v", err)
	}

	remote := httptest.NewRequest("GET", "/api/terminal", nil)
	remote.Header.Set("Origin", "https://outside.example")
	if err := validateTerminalOrigin(remote); err == nil || !strings.Contains(err.Error(), "must be local") {
		t.Fatalf("remote origin was accepted: %v", err)
	}
}
