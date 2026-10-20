package server

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/kube-haggis/kube-haggis/internal/kube"
	"golang.org/x/net/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

// terminalMessage is the JSON protocol shared by the browser and terminal bridge.
type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Cols    uint16 `json:"cols,omitempty"`
}

// terminalBridge adapts browser WebSocket messages to remotecommand streams.
type terminalBridge struct {
	socket    *websocket.Conn
	input     chan []byte
	sizes     chan remotecommand.TerminalSize
	done      chan struct{}
	pending   []byte
	closeOnce sync.Once
}

// terminalHandler returns the local WebSocket handler for Pod sessions.
func (s *Server) terminalHandler() http.Handler {
	return websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			return validateTerminalOrigin(request)
		},
		Handler: websocket.Handler(s.handleTerminal),
	}
}

// handleTerminal bridges one browser terminal session to Kubernetes.
func (s *Server) handleTerminal(socket *websocket.Conn) {
	bridge := newTerminalBridge(socket)
	defer bridge.Close()

	request := socket.Request()
	query := request.URL.Query()
	mode := kube.PodSessionMode(query.Get("mode"))
	command := []string(nil)
	if mode == kube.PodSessionExec {
		command = terminalCommand(query.Get("command"))
	}
	streamErr := s.kube.StreamPodSession(
		request.Context(),
		query.Get("context"),
		query.Get("namespace"),
		query.Get("pod"),
		query.Get("container"),
		mode,
		command,
		bridge,
		bridge,
		bridge,
	)
	if streamErr != nil {
		_ = bridge.send(terminalMessage{Type: "error", Message: streamErr.Error()})
		return
	}
	_ = bridge.send(terminalMessage{Type: "closed"})
}

// newTerminalBridge starts receiving browser terminal messages.
func newTerminalBridge(socket *websocket.Conn) *terminalBridge {
	bridge := &terminalBridge{
		socket: socket,
		input:  make(chan []byte, 16),
		sizes:  make(chan remotecommand.TerminalSize, 4),
		done:   make(chan struct{}),
	}
	go bridge.receiveMessages()
	return bridge
}

// receiveMessages routes browser input and resize messages to session channels.
func (b *terminalBridge) receiveMessages() {
	defer close(b.input)
	defer close(b.sizes)
	for {
		var message terminalMessage
		if err := websocket.JSON.Receive(b.socket, &message); err != nil {
			return
		}
		switch message.Type {
		case "input":
			if message.Data != "" {
				select {
				case b.input <- []byte(message.Data):
				case <-b.done:
					return
				}
			}
		case "resize":
			if message.Rows > 0 && message.Cols > 0 {
				select {
				case b.sizes <- remotecommand.TerminalSize{Width: message.Cols, Height: message.Rows}:
				case <-b.done:
					return
				}
			}
		case "close":
			return
		}
	}
}

// Read supplies browser keystrokes to the Kubernetes remote command stream.
func (b *terminalBridge) Read(buffer []byte) (int, error) {
	for len(b.pending) == 0 {
		input, open := <-b.input
		if !open {
			return 0, io.EOF
		}
		b.pending = input
	}
	count := copy(buffer, b.pending)
	b.pending = b.pending[count:]
	return count, nil
}

// Write sends Kubernetes terminal output to the browser as a JSON message.
func (b *terminalBridge) Write(output []byte) (int, error) {
	if err := b.send(terminalMessage{Type: "output", Data: string(output)}); err != nil {
		return 0, err
	}
	return len(output), nil
}

// Next returns the next browser-provided terminal size.
func (b *terminalBridge) Next() *remotecommand.TerminalSize {
	size, open := <-b.sizes
	if !open {
		return nil
	}
	return &size
}

// send writes one terminal protocol message to the browser.
func (b *terminalBridge) send(message terminalMessage) error {
	return websocket.JSON.Send(b.socket, message)
}

// Close closes the browser terminal connection once.
func (b *terminalBridge) Close() error {
	var err error
	b.closeOnce.Do(func() {
		close(b.done)
		err = b.socket.Close()
	})
	return err
}

// terminalCommand converts the command field into Kubernetes argv values.
func terminalCommand(value string) []string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return []string{"/bin/sh"}
	}
	return fields
}

// validateTerminalOrigin accepts browser origins served from the local machine.
func validateTerminalOrigin(request *http.Request) error {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid terminal origin")
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return nil
	default:
		return fmt.Errorf("terminal origin must be local")
	}
}
