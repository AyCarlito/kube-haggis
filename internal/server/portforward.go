package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/websocket"
)

// portForwardRequest contains the validated coordinates for one browser session.
type portForwardRequest struct {
	contextName string
	namespace   string
	pod         string
	targetPort  int
	localPort   int
}

// portForwardMessage is the text control protocol for one port-forward session.
type portForwardMessage struct {
	Type       string `json:"type"`
	Message    string `json:"message,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
	LocalPort  int    `json:"localPort,omitempty"`
}

// portForwardHandler returns the local WebSocket handler for Pod port-forward sessions.
func (s *Server) portForwardHandler() http.Handler {
	return websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			if err := validatePortForwardOrigin(request); err != nil {
				return err
			}
			_, err := parsePortForwardRequest(request)
			return err
		},
		Handler: websocket.Handler(s.handlePortForward),
	}
}

// handlePortForward starts a native Pod forward and keeps it alive for the browser session.
func (s *Server) handlePortForward(socket *websocket.Conn) {
	request := socket.Request()
	forwardRequest, err := parsePortForwardRequest(request)
	if err != nil {
		_ = sendPortForwardMessage(socket, portForwardMessage{Type: "error", Message: err.Error()})
		return
	}

	stopForwarding := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { close(stopForwarding) })
	}
	defer stop()

	ready := make(chan struct{})
	forwardErrors := make(chan error, 1)
	go func() {
		forwardErrors <- s.kube.ForwardPodPort(
			request.Context(),
			forwardRequest.contextName,
			forwardRequest.namespace,
			forwardRequest.pod,
			forwardRequest.targetPort,
			forwardRequest.localPort,
			stopForwarding,
			ready,
		)
	}()

	select {
	case <-ready:
	case forwardErr := <-forwardErrors:
		message := "port-forward session closed before listeners became ready"
		if forwardErr != nil {
			message = forwardErr.Error()
		}
		_ = sendPortForwardMessage(socket, portForwardMessage{Type: "error", Message: message})
		return
	case <-request.Context().Done():
		return
	}

	if err := sendPortForwardMessage(socket, portForwardMessage{
		Type:       "ready",
		TargetPort: forwardRequest.targetPort,
		LocalPort:  forwardRequest.localPort,
	}); err != nil {
		return
	}

	sessionErr, browserClosed := waitForPortForwardSession(socket, forwardErrors, request.Context().Done())
	if browserClosed {
		return
	}
	if sessionErr != nil && !isPortForwardNormalError(sessionErr) {
		_ = sendPortForwardMessage(socket, portForwardMessage{Type: "error", Message: sessionErr.Error()})
	}
	_ = sendPortForwardMessage(socket, portForwardMessage{Type: "closed"})
}

// parsePortForwardRequest validates the browser-facing port-forward query parameters.
func parsePortForwardRequest(request *http.Request) (portForwardRequest, error) {
	query := request.URL.Query()
	result := portForwardRequest{
		contextName: strings.TrimSpace(query.Get("context")),
		namespace:   strings.TrimSpace(query.Get("namespace")),
		pod:         strings.TrimSpace(query.Get("pod")),
	}
	if result.namespace == "" || result.namespace == "all" {
		return portForwardRequest{}, fmt.Errorf("namespace is required for Pod port forwarding")
	}
	if result.pod == "" {
		return portForwardRequest{}, fmt.Errorf("Pod name is required for port forwarding")
	}

	var err error
	result.targetPort, err = parsePortForwardPort(query.Get("targetPort"), "target port")
	if err != nil {
		return portForwardRequest{}, err
	}
	result.localPort, err = parsePortForwardPort(query.Get("localPort"), "local port")
	if err != nil {
		return portForwardRequest{}, err
	}
	return result, nil
}

// parsePortForwardPort parses one required TCP port from a query value.
func parsePortForwardPort(value, label string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("%s is required", label)
	}
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be between 1 and 65535", label)
	}
	return port, nil
}

// waitForPortForwardSession waits for the local browser session or Kubernetes forward to close.
func waitForPortForwardSession(socket *websocket.Conn, forwardErrors <-chan error, contextDone <-chan struct{}) (error, bool) {
	browserClosed := make(chan error, 1)
	go func() {
		var message []byte
		browserClosed <- websocket.Message.Receive(socket, &message)
	}()

	select {
	case err := <-browserClosed:
		return err, true
	case err := <-forwardErrors:
		return err, false
	case <-contextDone:
		return context.Canceled, false
	}
}

// sendPortForwardMessage sends one text control message to the browser.
func sendPortForwardMessage(socket *websocket.Conn, message portForwardMessage) error {
	return websocket.JSON.Send(socket, message)
}

// isPortForwardNormalError identifies disconnects that do not need an error banner.
func isPortForwardNormalError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "use of closed network connection") || strings.Contains(message, "websocket: close")
}

// validatePortForwardOrigin accepts browser origins served from the local machine.
func validatePortForwardOrigin(request *http.Request) error {
	return validateLocalOrigin(request, "port-forward")
}
