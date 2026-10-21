package server

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/AyCarlito/kube-haggis/internal/kube"
)

const (
	// maxPodCopyRequestSize leaves multipart framing room around the native copy limit.
	maxPodCopyRequestSize int64 = (64 << 20) + (1 << 20)
)

// ephemeralContainerPayload is the browser-facing debug-container request body.
type ephemeralContainerPayload struct {
	Name                string   `json:"name"`
	Image               string   `json:"image"`
	Command             []string `json:"command"`
	TargetContainerName string   `json:"targetContainerName"`
}

// handleEphemeralContainer adds one debug container through the native Pod subresource.
func (s *Server) handleEphemeralContainer(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required to launch an ephemeral debug container"})
		return
	}
	body, err := readBody(request)
	if err != nil {
		writeError(writer, err)
		return
	}
	var payload ephemeralContainerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, fmt.Errorf("decode ephemeral debug container request: %w", err))
		return
	}
	query := request.URL.Query()
	operation := "ephemeral-container"
	item, err := s.kube.AddEphemeralContainer(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("pod"), kube.EphemeralContainerRequest{
		Name:                payload.Name,
		Image:               payload.Image,
		Command:             payload.Command,
		TargetContainerName: payload.TargetContainerName,
	})
	if err != nil {
		s.recordMutation(request, operation, "", "v1", "pods", query.Get("namespace"), query.Get("pod"), false, false, "ephemeral debug container launch failed")
		writeError(writer, err)
		return
	}
	s.recordMutation(request, operation, "", "v1", "pods", query.Get("namespace"), item.Name, false, true, "ephemeral debug container launched")
	writeJSON(writer, http.StatusOK, map[string]string{"status": "launched", "pod": item.Name, "container": payload.Name})
}

// handlePodCopy serves read-only downloads and write-mode uploads through native Pod exec.
func (s *Server) handlePodCopy(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		s.handlePodCopyDownload(writer, request)
	case http.MethodPost:
		s.handlePodCopyUpload(writer, request)
	default:
		writer.Header().Set("Allow", "GET, POST")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handlePodCopyDownload streams a bounded regular file to the browser in read-only mode.
func (s *Server) handlePodCopyDownload(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	remotePath := query.Get("path")
	data, err := s.kube.CopyFileFromPod(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("pod"), query.Get("container"), remotePath)
	if err != nil {
		writeError(writer, err)
		return
	}

	filename := path.Base(path.Clean(strings.TrimSpace(remotePath)))
	if contentDisposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename}); contentDisposition != "" {
		writer.Header().Set("Content-Disposition", contentDisposition)
	}
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(data)
}

// handlePodCopyUpload accepts a local multipart file and writes it to a container in write mode.
func (s *Server) handlePodCopyUpload(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required to upload a file into a Pod container"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxPodCopyRequestSize)
	if err := request.ParseMultipartForm(maxPodCopyRequestSize); err != nil {
		writeError(writer, fmt.Errorf("parse Pod file upload: %w", err))
		return
	}
	if request.MultipartForm != nil {
		defer request.MultipartForm.RemoveAll()
	}
	file, header, err := request.FormFile("file")
	if err != nil {
		writeError(writer, fmt.Errorf("file is required for Pod upload: %w", err))
		return
	}
	defer file.Close()
	if header.Size < 0 {
		writeError(writer, fmt.Errorf("uploaded file size is invalid"))
		return
	}

	query := request.URL.Query()
	namespace := query.Get("namespace")
	podName := query.Get("pod")
	operation := "copy-upload"
	err = s.kube.CopyFileToPod(request.Context(), query.Get("context"), namespace, podName, request.FormValue("container"), request.FormValue("path"), file, header.Size)
	if err != nil {
		s.recordMutation(request, operation, "", "v1", "pods", namespace, podName, false, false, "Pod file upload failed")
		writeError(writer, err)
		return
	}
	s.recordMutation(request, operation, "", "v1", "pods", namespace, podName, false, true, "Pod file uploaded")
	writeJSON(writer, http.StatusOK, map[string]string{"status": "uploaded", "pod": podName})
}
