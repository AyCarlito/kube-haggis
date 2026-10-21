// Command kube-haggis starts the local KubeHaggis web console.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/AyCarlito/kube-haggis/internal/audit"
	"github.com/AyCarlito/kube-haggis/internal/kube"
	"github.com/AyCarlito/kube-haggis/internal/server"
)

// main configures and starts the localhost KubeHaggis server.
func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	kubeconfig := flag.String("kubeconfig", "", "optional explicit kubeconfig path")
	webDir := flag.String("web-dir", "web/dist", "built frontend directory")
	auditPath := flag.String("audit-db", defaultAuditPath(), "local SQLite mutation history path")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	kubernetes := kube.NewService(*kubeconfig)
	auditStore, auditErr := audit.Open(*auditPath)
	if auditErr != nil {
		logger.Warn("mutation history disabled", "error", auditErr)
	}
	if auditStore != nil {
		defer auditStore.Close()
	}
	handler := server.New(kubernetes, *webDir, logger, auditStore).Handler()
	logger.Info("starting KubeHaggis", "address", *address, "webDir", *webDir)
	if err := http.ListenAndServe(*address, handler); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// defaultAuditPath returns the platform configuration path for local history.
func defaultAuditPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "kube-haggis.db"
	}
	return filepath.Join(configDir, "kube-haggis", "audit.db")
}
