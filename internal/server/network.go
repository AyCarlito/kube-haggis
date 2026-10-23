package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AyCarlito/kube-haggis/internal/kube"
)

const (
	networkCaptureSampleType    = "sample"
	networkCapturePeersType     = "peers"
	networkCaptureStatusType    = "status"
	networkCaptureErrorType     = "error"
	networkCaptureClosedType    = "closed"
	networkCaptureFlowCap       = 128
	networkCapturePeerCap       = 48
	networkPeerDiscoveryTimeout = 5 * time.Second
)

// networkPrepareResponse acknowledges the reusable debug container and returns peer labels.
type networkPrepareResponse struct {
	Status    string                             `json:"status"`
	Container string                             `json:"container"`
	Image     string                             `json:"image"`
	Created   bool                               `json:"created"`
	Peers     map[string]kube.NetworkPodIdentity `json:"peers,omitempty"`
}

// networkCaptureEvent is the safe server-sent event envelope for one live Pod capture.
type networkCaptureEvent struct {
	Type               string                `json:"type"`
	State              string                `json:"state,omitempty"`
	Message            string                `json:"message,omitempty"`
	At                 time.Time             `json:"at,omitempty"`
	Container          string                `json:"container,omitempty"`
	MaxDurationSeconds int                   `json:"maxDurationSeconds,omitempty"`
	Sample             *networkCaptureSample `json:"sample,omitempty"`
	Peers              []networkPeerSnapshot `json:"peers,omitempty"`
}

// networkCaptureSample contains exact interface-counter deltas for one chart point.
type networkCaptureSample struct {
	InboundBytesPerSecond    float64 `json:"inboundBytesPerSecond"`
	OutboundBytesPerSecond   float64 `json:"outboundBytesPerSecond"`
	InboundPacketsPerSecond  float64 `json:"inboundPacketsPerSecond"`
	OutboundPacketsPerSecond float64 `json:"outboundPacketsPerSecond"`
	InboundBytesTotal        uint64  `json:"inboundBytesTotal"`
	OutboundBytesTotal       uint64  `json:"outboundBytesTotal"`
	InboundPacketsTotal      uint64  `json:"inboundPacketsTotal"`
	OutboundPacketsTotal     uint64  `json:"outboundPacketsTotal"`
	InboundErrors            uint64  `json:"inboundErrors"`
	OutboundErrors           uint64  `json:"outboundErrors"`
	InboundDrops             uint64  `json:"inboundDrops"`
	OutboundDrops            uint64  `json:"outboundDrops"`
}

// networkPeerSnapshot contains one bounded cumulative flow summary for the Pod-local map.
type networkPeerSnapshot struct {
	Source          string    `json:"source"`
	Destination     string    `json:"destination"`
	SourcePort      int       `json:"sourcePort,omitempty"`
	DestinationPort int       `json:"destinationPort,omitempty"`
	Protocol        string    `json:"protocol"`
	Packets         uint64    `json:"packets"`
	PayloadBytes    uint64    `json:"payloadBytes"`
	LastSeen        time.Time `json:"lastSeen"`
}

// networkCaptureLine is one complete line from the multiplexed capture command.
type networkCaptureLine struct {
	Source string
	Text   string
}

// networkInterfaceStats contains the counters emitted by /proc/net/dev.
type networkInterfaceStats struct {
	InboundBytes    uint64
	InboundPackets  uint64
	InboundErrors   uint64
	InboundDrops    uint64
	OutboundBytes   uint64
	OutboundPackets uint64
	OutboundErrors  uint64
	OutboundDrops   uint64
}

// networkPacket contains the safe L3/L4 fields parsed from one tcpdump summary.
type networkPacket struct {
	Source          string
	Destination     string
	SourcePort      int
	DestinationPort int
	Protocol        string
	PayloadBytes    uint64
}

// networkFlowKey identifies one directional protocol and port tuple.
type networkFlowKey struct {
	Source          string
	Destination     string
	SourcePort      int
	DestinationPort int
	Protocol        string
}

// networkFlowState stores the bounded cumulative state for one observed flow.
type networkFlowState struct {
	Key          networkFlowKey
	Packets      uint64
	PayloadBytes uint64
	LastSeen     time.Time
}

// networkCaptureAggregator converts command output into bounded chart and peer events.
type networkCaptureAggregator struct {
	baseStats     *networkInterfaceStats
	previousStats *networkInterfaceStats
	previousAt    time.Time
	flows         map[networkFlowKey]*networkFlowState
	hasError      bool
}

// newNetworkCaptureAggregator creates an empty bounded capture reducer.
func newNetworkCaptureAggregator() *networkCaptureAggregator {
	return &networkCaptureAggregator{flows: make(map[networkFlowKey]*networkFlowState)}
}

// handleNetworkPrepare creates or reuses the Pod-local network debug container.
func (s *Server) handleNetworkPrepare(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Header.Get("X-KubeHaggis-Write") != "enabled" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "write mode is required to start live network capture"})
		return
	}
	query := request.URL.Query()
	result, err := s.kube.EnsureNetworkDebugContainer(request.Context(), query.Get("context"), query.Get("namespace"), query.Get("pod"), s.networkDebugImage)
	if err != nil {
		s.recordMutation(request, "network-debug-container", "", "v1", "pods", query.Get("namespace"), query.Get("pod"), false, false, "network debug container preparation failed")
		writeError(writer, err)
		return
	}
	if result.Created {
		s.recordMutation(request, "network-debug-container", "", "v1", "pods", query.Get("namespace"), query.Get("pod"), false, true, "network debug container attached")
	}
	peerContext, peerCancel := context.WithTimeout(request.Context(), networkPeerDiscoveryTimeout)
	peers, _ := s.kube.NetworkPodIdentities(peerContext, query.Get("context"), query.Get("namespace"))
	peerCancel()
	writeJSON(writer, http.StatusOK, networkPrepareResponse{Status: "ready", Container: result.Name, Image: result.Image, Created: result.Created, Peers: peers})
}

// handleNetworkCapture streams one bounded live packet-header session from a reusable debug container.
func (s *Server) handleNetworkCapture(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	query := request.URL.Query()
	ctx, cancel := context.WithTimeout(request.Context(), kube.NetworkCaptureMaxDuration)
	defer cancel()
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, fmt.Errorf("network capture streaming is not supported by this server"))
		return
	}
	telemetry := requestTelemetryFor(request)
	sessionID := s.diagnosticStore().startSession("network", telemetry.Context, telemetry.Resource)
	defer func() {
		s.diagnosticStore().finishSession(sessionID, "", telemetry.Operation)
	}()
	if !writeWatchEvent(writer, flusher, networkCaptureEvent{Type: networkCaptureStatusType, State: "capturing", At: time.Now().UTC(), Container: kube.NetworkDebugContainerName, MaxDurationSeconds: int(kube.NetworkCaptureMaxDuration / time.Second)}) {
		return
	}
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	streamDone := make(chan error, 1)
	go func() {
		err := s.kube.StreamPodNetworkCapture(ctx, query.Get("context"), query.Get("namespace"), query.Get("pod"), kube.NetworkDebugContainerName, stdoutWriter, stderrWriter)
		_ = stdoutWriter.CloseWithError(err)
		_ = stderrWriter.CloseWithError(err)
		streamDone <- err
	}()
	lines := make(chan networkCaptureLine, 128)
	var scanners sync.WaitGroup
	scanners.Add(2)
	go scanNetworkCaptureLines(ctx, &scanners, stdoutReader, "stdout", lines)
	go scanNetworkCaptureLines(ctx, &scanners, stderrReader, "stderr", lines)
	go func() {
		scanners.Wait()
		close(lines)
	}()
	aggregator := newNetworkCaptureAggregator()
	peerTicker := time.NewTicker(time.Second)
	defer peerTicker.Stop()
	for {
		select {
		case line, open := <-lines:
			if !open {
				lines = nil
				continue
			}
			for _, event := range aggregator.consume(line) {
				if !writeWatchEvent(writer, flusher, event) {
					return
				}
			}
		case <-peerTicker.C:
			if !writeWatchEvent(writer, flusher, networkCaptureEvent{Type: networkCapturePeersType, At: time.Now().UTC(), Peers: aggregator.peers()}) {
				return
			}
		case streamErr := <-streamDone:
			if lines != nil {
				for line := range lines {
					for _, event := range aggregator.consume(line) {
						if !writeWatchEvent(writer, flusher, event) {
							return
						}
					}
				}
				lines = nil
			}
			if !writeWatchEvent(writer, flusher, networkCaptureEvent{Type: networkCapturePeersType, At: time.Now().UTC(), Peers: aggregator.peers()}) {
				return
			}
			if ctx.Err() == context.DeadlineExceeded {
				_ = writeWatchEvent(writer, flusher, networkCaptureEvent{Type: networkCaptureClosedType, State: "max-duration", Message: "The live network capture reached its safety limit."})
				return
			}
			if ctx.Err() != nil {
				return
			}
			if aggregator.hasError {
				s.diagnosticStore().finishSession(sessionID, "stream-error", telemetry.Operation)
				sessionID = ""
				return
			}
			if streamErr != nil {
				category := safeErrorCategory(streamErr)
				s.diagnosticStore().finishSession(sessionID, category, telemetry.Operation)
				sessionID = ""
				_ = writeWatchEvent(writer, flusher, networkCaptureEvent{Type: networkCaptureErrorType, State: category, Message: safeStreamErrorMessage(streamErr)})
				return
			}
			_ = writeWatchEvent(writer, flusher, networkCaptureEvent{Type: networkCaptureClosedType, State: "closed", Message: "The live network capture ended."})
			return
		}
	}
}

// scanNetworkCaptureLines forwards bounded command lines without retaining packet output.
func scanNetworkCaptureLines(ctx context.Context, scanners *sync.WaitGroup, reader io.Reader, source string, lines chan<- networkCaptureLine) {
	defer scanners.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for scanner.Scan() {
		select {
		case lines <- networkCaptureLine{Source: source, Text: scanner.Text()}:
		case <-ctx.Done():
			return
		}
	}
}

// consume reduces one command line into zero or more safe browser events.
func (aggregator *networkCaptureAggregator) consume(line networkCaptureLine) []networkCaptureEvent {
	if line.Source == "stderr" {
		if strings.TrimSpace(line.Text) == "" {
			return nil
		}
		aggregator.hasError = true
		return []networkCaptureEvent{{Type: networkCaptureErrorType, State: "capture-error", Message: "The network capture command reported an error."}}
	}
	if strings.HasPrefix(line.Text, "S\t") {
		stats, ok := parseNetworkInterfaceStats(line.Text)
		if !ok {
			return nil
		}
		at := time.Now().UTC()
		if aggregator.baseStats == nil {
			base := stats
			aggregator.baseStats = &base
		}
		sample := networkCaptureSample{
			InboundBytesTotal:    counterDelta(stats.InboundBytes, aggregator.baseStats.InboundBytes),
			OutboundBytesTotal:   counterDelta(stats.OutboundBytes, aggregator.baseStats.OutboundBytes),
			InboundPacketsTotal:  counterDelta(stats.InboundPackets, aggregator.baseStats.InboundPackets),
			OutboundPacketsTotal: counterDelta(stats.OutboundPackets, aggregator.baseStats.OutboundPackets),
			InboundErrors:        counterDelta(stats.InboundErrors, aggregator.baseStats.InboundErrors),
			OutboundErrors:       counterDelta(stats.OutboundErrors, aggregator.baseStats.OutboundErrors),
			InboundDrops:         counterDelta(stats.InboundDrops, aggregator.baseStats.InboundDrops),
			OutboundDrops:        counterDelta(stats.OutboundDrops, aggregator.baseStats.OutboundDrops),
		}
		if aggregator.previousStats != nil && !aggregator.previousAt.IsZero() {
			seconds := at.Sub(aggregator.previousAt).Seconds()
			if seconds > 0 {
				sample.InboundBytesPerSecond = counterRate(stats.InboundBytes, aggregator.previousStats.InboundBytes, seconds)
				sample.OutboundBytesPerSecond = counterRate(stats.OutboundBytes, aggregator.previousStats.OutboundBytes, seconds)
				sample.InboundPacketsPerSecond = counterRate(stats.InboundPackets, aggregator.previousStats.InboundPackets, seconds)
				sample.OutboundPacketsPerSecond = counterRate(stats.OutboundPackets, aggregator.previousStats.OutboundPackets, seconds)
			}
		}
		aggregator.previousStats = &stats
		aggregator.previousAt = at
		return []networkCaptureEvent{{Type: networkCaptureSampleType, At: at, Sample: &sample}}
	}
	if !strings.HasPrefix(line.Text, "P\t") {
		return nil
	}
	packet, ok := parseTcpdumpPacket(strings.TrimPrefix(line.Text, "P\t"))
	if !ok {
		if tcpdumpCaptureFailure(line.Text) {
			aggregator.hasError = true
			return []networkCaptureEvent{{Type: networkCaptureErrorType, State: "capture-error", Message: "tcpdump could not start in the Pod network namespace."}}
		}
		return nil
	}
	key := networkFlowKey{Source: packet.Source, Destination: packet.Destination, SourcePort: packet.SourcePort, DestinationPort: packet.DestinationPort, Protocol: packet.Protocol}
	flow, exists := aggregator.flows[key]
	if !exists {
		if len(aggregator.flows) >= networkCaptureFlowCap {
			return nil
		}
		flow = &networkFlowState{Key: key}
		aggregator.flows[key] = flow
	}
	flow.Packets++
	flow.PayloadBytes += packet.PayloadBytes
	flow.LastSeen = time.Now().UTC()
	return nil
}

// tcpdumpCaptureFailure distinguishes normal startup notices from capture failures.
func tcpdumpCaptureFailure(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if !strings.Contains(lower, "tcpdump:") {
		return false
	}
	if strings.Contains(lower, "verbose output suppressed") || strings.Contains(lower, "listening on ") {
		return false
	}
	for _, marker := range []string{"permission", "not permitted", "no such device", "cannot", "can't", "failed", "invalid", "not supported", "error"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// peers returns the highest-volume bounded flow summaries in a stable order.
func (aggregator *networkCaptureAggregator) peers() []networkPeerSnapshot {
	flows := make([]*networkFlowState, 0, len(aggregator.flows))
	for _, flow := range aggregator.flows {
		flows = append(flows, flow)
	}
	sort.Slice(flows, func(left, right int) bool {
		if flows[left].Packets != flows[right].Packets {
			return flows[left].Packets > flows[right].Packets
		}
		return flows[left].LastSeen.Before(flows[right].LastSeen)
	})
	if len(flows) > networkCapturePeerCap {
		flows = flows[:networkCapturePeerCap]
	}
	peers := make([]networkPeerSnapshot, 0, len(flows))
	for _, flow := range flows {
		peers = append(peers, networkPeerSnapshot{Source: flow.Key.Source, Destination: flow.Key.Destination, SourcePort: flow.Key.SourcePort, DestinationPort: flow.Key.DestinationPort, Protocol: flow.Key.Protocol, Packets: flow.Packets, PayloadBytes: flow.PayloadBytes, LastSeen: flow.LastSeen})
	}
	return peers
}

// counterRate converts a monotonic counter delta into a non-negative per-second rate.
func counterRate(current, previous uint64, seconds float64) float64 {
	if current < previous || seconds <= 0 {
		return 0
	}
	return float64(current-previous) / seconds
}

// counterDelta converts an interface counter into a session-relative total.
func counterDelta(current, base uint64) uint64 {
	if current < base {
		return current
	}
	return current - base
}

// parseNetworkInterfaceStats parses one machine-readable /proc/net/dev sample.
func parseNetworkInterfaceStats(line string) (networkInterfaceStats, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) != 9 || fields[0] != "S" {
		return networkInterfaceStats{}, false
	}
	values := make([]uint64, 8)
	for index := range values {
		value, err := strconv.ParseUint(strings.TrimSpace(fields[index+1]), 10, 64)
		if err != nil {
			return networkInterfaceStats{}, false
		}
		values[index] = value
	}
	return networkInterfaceStats{InboundBytes: values[0], InboundPackets: values[1], InboundErrors: values[2], InboundDrops: values[3], OutboundBytes: values[4], OutboundPackets: values[5], OutboundErrors: values[6], OutboundDrops: values[7]}, true
}

// parseTcpdumpPacket extracts bounded address, port, protocol, and payload fields from tcpdump text.
func parseTcpdumpPacket(line string) (networkPacket, bool) {
	protocolIndex := strings.Index(line, " IP6 ")
	protocolLength := len(" IP6 ")
	if protocolIndex < 0 {
		protocolIndex = strings.Index(line, " IP ")
		protocolLength = len(" IP ")
	}
	if protocolIndex < 0 {
		return networkPacket{}, false
	}
	body := line[protocolIndex+protocolLength:]
	separator := strings.Index(body, " > ")
	if separator < 0 {
		return networkPacket{}, false
	}
	source, sourcePort := parseTcpdumpEndpoint(body[:separator])
	destinationBody := body[separator+3:]
	destinationEnd := strings.Index(destinationBody, ": ")
	if destinationEnd < 0 {
		return networkPacket{}, false
	}
	destination, destinationPort := parseTcpdumpEndpoint(destinationBody[:destinationEnd])
	tail := strings.TrimSpace(destinationBody[destinationEnd+2:])
	protocol := tcpdumpProtocol(tail)
	if source == "" || destination == "" || protocol == "" {
		return networkPacket{}, false
	}
	return networkPacket{Source: source, Destination: destination, SourcePort: sourcePort, DestinationPort: destinationPort, Protocol: protocol, PayloadBytes: tcpdumpPayloadLength(tail)}, true
}

// parseTcpdumpEndpoint separates the final numeric port from a tcpdump address token.
func parseTcpdumpEndpoint(value string) (string, int) {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	lastDot := strings.LastIndexByte(value, '.')
	if lastDot < 0 || lastDot == len(value)-1 {
		return value, 0
	}
	port, err := strconv.Atoi(value[lastDot+1:])
	if err != nil {
		return value, 0
	}
	return value[:lastDot], port
}

// tcpdumpProtocol normalizes the common transport tokens emitted by tcpdump.
func tcpdumpProtocol(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "tcp"), strings.Contains(lower, "flags"):
		return "TCP"
	case strings.Contains(lower, "udp"):
		return "UDP"
	case strings.Contains(lower, "icmp6"):
		return "ICMPv6"
	case strings.Contains(lower, "icmp"):
		return "ICMP"
	default:
		return "IP"
	}
}

// tcpdumpPayloadLength extracts the optional payload length printed by tcpdump.
func tcpdumpPayloadLength(value string) uint64 {
	marker := "length "
	index := strings.LastIndex(value, marker)
	if index < 0 {
		return 0
	}
	fields := strings.Fields(value[index+len(marker):])
	if len(fields) == 0 {
		return 0
	}
	length, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return length
}
