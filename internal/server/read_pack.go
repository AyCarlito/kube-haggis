package server

import (
	"context"
	"net/http"

	"github.com/AyCarlito/kube-haggis/internal/kube"
)

// operationalReadPackLoader describes the bounded, read-only operational summary seam.
type operationalReadPackLoader func(context.Context, string, string, string, string, string, string, bool) (kube.OperationalReadPack, error)

// handleOperationalReadPack returns one exact-GVR operational read pack without mutation controls.
func (s *Server) handleOperationalReadPack(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	query := request.URL.Query()
	loadPack := s.readPack
	if loadPack == nil {
		if s.kube == nil {
			writeOperationalReadPackError(writer)
			return
		}
		loadPack = s.kube.OperationalReadPack
	}
	pack, err := loadPack(
		request.Context(),
		query.Get("context"),
		query.Get("group"),
		query.Get("version"),
		query.Get("resource"),
		query.Get("namespace"),
		query.Get("name"),
		query.Get("namespaced") != "false",
	)
	if err != nil {
		writeOperationalReadPackError(writer)
		return
	}
	writeJSON(writer, http.StatusOK, pack)
}

// writeOperationalReadPackError keeps setup and coordinate failures free of raw Kubernetes detail.
func writeOperationalReadPackError(writer http.ResponseWriter) {
	writeJSON(writer, http.StatusBadGateway, map[string]string{
		"error": "the operational read pack is unavailable; generic YAML, Events, and related resources remain available",
	})
}
