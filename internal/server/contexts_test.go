package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kube-haggis/kube-haggis/internal/kube"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// writeServerTestConfig writes a kubeconfig containing no data that should reach the browser.
func writeServerTestConfig(t *testing.T, path string, contextNames ...string) {
	t.Helper()
	config := clientcmdapi.NewConfig()
	if len(contextNames) > 0 {
		config.CurrentContext = contextNames[0]
	}
	for _, contextName := range contextNames {
		clusterName := contextName + "-cluster"
		userName := contextName + "-user"
		config.Contexts[contextName] = &clientcmdapi.Context{Cluster: clusterName, AuthInfo: userName}
		config.Clusters[clusterName] = &clientcmdapi.Cluster{Server: "https://" + contextName + ".example.test"}
		config.AuthInfos[userName] = &clientcmdapi.AuthInfo{Token: "server-test-secret"}
	}
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatalf("write server test kubeconfig: %v", err)
	}
}

// TestContextsAPIReportsReloadsWithoutSensitiveData verifies GET polling and POST reload behavior.
func TestContextsAPIReportsReloadsWithoutSensitiveData(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeServerTestConfig(t, configPath, "primary")
	t.Setenv("KUBECONFIG", configPath)
	handler := (&Server{kube: kube.NewService("")}).Handler()

	firstRequest := httptest.NewRequest(http.MethodGet, "/api/contexts", nil)
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, firstRequest)
	if firstResponse.Code != http.StatusOK || strings.Contains(firstResponse.Body.String(), "server-test-secret") {
		t.Fatalf("initial context API response was unsafe: status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	var firstPayload map[string]interface{}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode initial context response: %v", err)
	}
	if firstPayload["sourceState"] != "ready" || firstPayload["revision"] != float64(1) {
		t.Fatalf("unexpected initial context state: %+v", firstPayload)
	}

	writeServerTestConfig(t, configPath, "primary", "secondary")
	changedRequest := httptest.NewRequest(http.MethodGet, "/api/contexts", nil)
	changedResponse := httptest.NewRecorder()
	handler.ServeHTTP(changedResponse, changedRequest)
	if changedResponse.Code != http.StatusOK || !strings.Contains(changedResponse.Body.String(), "secondary") || strings.Contains(changedResponse.Body.String(), "server-test-secret") {
		t.Fatalf("changed context API response was incorrect or unsafe: status=%d body=%s", changedResponse.Code, changedResponse.Body.String())
	}
	var changedPayload map[string]interface{}
	if err := json.Unmarshal(changedResponse.Body.Bytes(), &changedPayload); err != nil {
		t.Fatalf("decode changed context response: %v", err)
	}
	if changedPayload["sourceState"] != "changed" || changedPayload["reloaded"] != true || changedPayload["revision"] != float64(2) {
		t.Fatalf("changed context state was not reported: %+v", changedPayload)
	}

	reloadRequest := httptest.NewRequest(http.MethodPost, "/api/contexts/reload", nil)
	reloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(reloadResponse, reloadRequest)
	if reloadResponse.Code != http.StatusOK || !strings.Contains(reloadResponse.Body.String(), `"reloaded":true`) || strings.Contains(reloadResponse.Body.String(), "server-test-secret") {
		t.Fatalf("explicit reload API response was incorrect or unsafe: status=%d body=%s", reloadResponse.Code, reloadResponse.Body.String())
	}
}

// TestContextsAPIKeepsLastGoodStateOnInvalidReload verifies source errors are safe and recoverable.
func TestContextsAPIKeepsLastGoodStateOnInvalidReload(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeServerTestConfig(t, configPath, "primary")
	server := &Server{kube: kube.NewService(configPath)}

	initialResponse := httptest.NewRecorder()
	server.handleContexts(initialResponse, httptest.NewRequest(http.MethodGet, "/api/contexts", nil))
	if initialResponse.Code != http.StatusOK {
		t.Fatalf("initial context handler failed: %d", initialResponse.Code)
	}

	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nclusters: [handler-reload-secret\n"), 0600); err != nil {
		t.Fatalf("corrupt server test kubeconfig: %v", err)
	}
	failedResponse := httptest.NewRecorder()
	server.handleContextsReload(failedResponse, httptest.NewRequest(http.MethodPost, "/api/contexts/reload", nil))
	if failedResponse.Code != http.StatusOK || !strings.Contains(failedResponse.Body.String(), `"sourceState":"error"`) || !strings.Contains(failedResponse.Body.String(), "primary") || strings.Contains(failedResponse.Body.String(), "handler-reload-secret") {
		t.Fatalf("invalid reload response was unsafe or discarded good state: status=%d body=%s", failedResponse.Code, failedResponse.Body.String())
	}

	writeServerTestConfig(t, configPath, "recovered")
	recoveredResponse := httptest.NewRecorder()
	server.handleContexts(recoveredResponse, httptest.NewRequest(http.MethodGet, "/api/contexts", nil))
	if recoveredResponse.Code != http.StatusOK || !strings.Contains(recoveredResponse.Body.String(), "recovered") || strings.Contains(recoveredResponse.Body.String(), "handler-reload-secret") || strings.Contains(recoveredResponse.Body.String(), "reloadError") {
		t.Fatalf("recovered reload did not clear stale error: status=%d body=%s", recoveredResponse.Code, recoveredResponse.Body.String())
	}
}
