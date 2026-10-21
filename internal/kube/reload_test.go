package kube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// writeReloadTestConfig writes a small kubeconfig with distinct safe context identities.
func writeReloadTestConfig(t *testing.T, path, current string, contextNames ...string) {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.CurrentContext = current
	for _, contextName := range contextNames {
		clusterName := contextName + "-cluster"
		userName := contextName + "-user"
		config.Contexts[contextName] = &clientcmdapi.Context{Cluster: clusterName, AuthInfo: userName}
		config.Clusters[clusterName] = &clientcmdapi.Cluster{Server: "https://" + contextName + ".example.test"}
		config.AuthInfos[userName] = &clientcmdapi.AuthInfo{Token: "reload-test-secret"}
	}
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatalf("write test kubeconfig %q: %v", path, err)
	}
}

// TestServiceReloadsMultipleKubeconfigSources verifies standard multi-file loading and metadata detection.
func TestServiceReloadsMultipleKubeconfigSources(t *testing.T) {
	tempDir := t.TempDir()
	firstPath := filepath.Join(tempDir, "first.yaml")
	secondPath := filepath.Join(tempDir, "second.yaml")
	writeReloadTestConfig(t, firstPath, "primary", "primary")
	writeReloadTestConfig(t, secondPath, "secondary", "secondary")
	t.Setenv("KUBECONFIG", firstPath+string(os.PathListSeparator)+secondPath)

	service := NewService("")
	initial, err := service.Contexts()
	if err != nil {
		t.Fatalf("load initial multi-file config: %v", err)
	}
	if len(initial.Contexts) != 2 || initial.Kubeconfig.Revision != 1 || initial.Kubeconfig.State != kubeconfigStateReady {
		t.Fatalf("unexpected initial context snapshot: %+v", initial)
	}

	writeReloadTestConfig(t, secondPath, "secondary", "secondary", "added")
	changed, err := service.Contexts()
	if err != nil {
		t.Fatalf("reload changed multi-file config: %v", err)
	}
	if len(changed.Contexts) != 3 || changed.Kubeconfig.State != kubeconfigStateChanged || !changed.Kubeconfig.Reloaded || changed.Kubeconfig.Revision != 2 {
		t.Fatalf("changed source was not reloaded: %+v", changed)
	}

	if err := os.Chtimes(firstPath, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("change kubeconfig metadata: %v", err)
	}
	metadataChanged, err := service.Contexts()
	if err != nil {
		t.Fatalf("reload metadata change: %v", err)
	}
	if metadataChanged.Kubeconfig.State != kubeconfigStateChanged || metadataChanged.Kubeconfig.Revision != 3 {
		t.Fatalf("metadata change did not trigger reload: %+v", metadataChanged.Kubeconfig)
	}

	encoded, err := json.Marshal(metadataChanged)
	if err != nil {
		t.Fatalf("encode safe context snapshot: %v", err)
	}
	if strings.Contains(string(encoded), "reload-test-secret") {
		t.Fatalf("context snapshot exposed kubeconfig credentials: %s", encoded)
	}
}

// TestServiceReloadsWhenKubeconfigSourceAppears verifies missing KUBECONFIG entries are fingerprinted.
func TestServiceReloadsWhenKubeconfigSourceAppears(t *testing.T) {
	tempDir := t.TempDir()
	firstPath := filepath.Join(tempDir, "first.yaml")
	appearingPath := filepath.Join(tempDir, "appearing.yaml")
	writeReloadTestConfig(t, firstPath, "primary", "primary")
	t.Setenv("KUBECONFIG", firstPath+string(os.PathListSeparator)+appearingPath)

	service := NewService("")
	initial, err := service.Contexts()
	if err != nil || len(initial.Contexts) != 1 {
		t.Fatalf("load config with missing secondary source: contexts=%+v err=%v", initial, err)
	}

	writeReloadTestConfig(t, appearingPath, "secondary", "secondary")
	changed, err := service.Contexts()
	if err != nil {
		t.Fatalf("reload after secondary source appeared: %v", err)
	}
	if len(changed.Contexts) != 2 || changed.Kubeconfig.State != kubeconfigStateChanged || changed.Kubeconfig.Revision != 2 {
		t.Fatalf("appearing source was not loaded: %+v", changed)
	}
}

// TestServiceReloadsWhenKubeconfigEnvironmentChanges verifies the source list is rebuilt from KUBECONFIG.
func TestServiceReloadsWhenKubeconfigEnvironmentChanges(t *testing.T) {
	tempDir := t.TempDir()
	firstPath := filepath.Join(tempDir, "first.yaml")
	secondPath := filepath.Join(tempDir, "second.yaml")
	writeReloadTestConfig(t, firstPath, "primary", "primary")
	writeReloadTestConfig(t, secondPath, "secondary", "secondary")
	t.Setenv("KUBECONFIG", firstPath)

	service := NewService("")
	initial, err := service.Contexts()
	if err != nil || len(initial.Contexts) != 1 || initial.Contexts[0].Name != "primary" {
		t.Fatalf("load initial environment source: contexts=%+v err=%v", initial, err)
	}

	t.Setenv("KUBECONFIG", secondPath)
	changed, err := service.Contexts()
	if err != nil || len(changed.Contexts) != 1 || changed.Contexts[0].Name != "secondary" {
		t.Fatalf("reload changed environment source: contexts=%+v err=%v", changed, err)
	}
	if changed.Kubeconfig.State != kubeconfigStateChanged || changed.Kubeconfig.Revision != 2 {
		t.Fatalf("environment source change was not reported: %+v", changed.Kubeconfig)
	}
}

// TestServiceRetainsLastGoodConfigOnReloadError verifies failed reloads do not discard usable state.
func TestServiceRetainsLastGoodConfigOnReloadError(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	writeReloadTestConfig(t, configPath, "primary", "primary")

	service := NewService(configPath)
	initial, err := service.Contexts()
	if err != nil || len(initial.Contexts) != 1 {
		t.Fatalf("load initial explicit config: contexts=%+v err=%v", initial, err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nclusters: [reload-error-secret\n"), 0600); err != nil {
		t.Fatalf("corrupt kubeconfig: %v", err)
	}

	failed, err := service.Contexts()
	if err != nil {
		t.Fatalf("last-good context should remain usable: %v", err)
	}
	if len(failed.Contexts) != 1 || failed.Contexts[0].Name != "primary" || failed.Kubeconfig.State != kubeconfigStateError {
		t.Fatalf("failed reload did not retain old state: %+v", failed)
	}
	if failed.Kubeconfig.Error == "" || strings.Contains(failed.Kubeconfig.Error, "reload-error-secret") || strings.Contains(failed.Kubeconfig.Error, configPath) {
		t.Fatalf("unsafe or missing reload error: %+v", failed.Kubeconfig)
	}

	reloaded, err := service.ReloadContexts()
	if err != nil {
		t.Fatalf("forced reload should retain the last-good snapshot: %v", err)
	}
	if len(reloaded.Contexts) != 1 || reloaded.Kubeconfig.State != kubeconfigStateError {
		t.Fatalf("forced failed reload changed the safe snapshot: %+v", reloaded)
	}

	writeReloadTestConfig(t, configPath, "recovered", "recovered")
	recovered, err := service.Contexts()
	if err != nil || len(recovered.Contexts) != 1 || recovered.Contexts[0].Name != "recovered" {
		t.Fatalf("reload after source repair failed: contexts=%+v err=%v", recovered, err)
	}
	if recovered.Kubeconfig.State != kubeconfigStateChanged || recovered.Kubeconfig.Error != "" {
		t.Fatalf("stale reload error was not cleared: %+v", recovered.Kubeconfig)
	}
}

// TestServiceForcedReloadAdvancesRevision verifies the explicit reload control reloads unchanged metadata.
func TestServiceForcedReloadAdvancesRevision(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeReloadTestConfig(t, configPath, "primary", "primary")
	service := NewService(configPath)
	initial, err := service.Contexts()
	if err != nil {
		t.Fatalf("load initial explicit config: %v", err)
	}

	reloaded, err := service.ReloadContexts()
	if err != nil {
		t.Fatalf("force reload unchanged config: %v", err)
	}
	if reloaded.Kubeconfig.State != kubeconfigStateChanged || !reloaded.Kubeconfig.Reloaded || reloaded.Kubeconfig.Revision != initial.Kubeconfig.Revision+1 {
		t.Fatalf("explicit reload did not advance state: initial=%+v reloaded=%+v", initial.Kubeconfig, reloaded.Kubeconfig)
	}
}
