package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestParseNetworkInterfaceStats accepts the fixed counter line emitted by the debug shell.
func TestParseNetworkInterfaceStats(t *testing.T) {
	stats, ok := parseNetworkInterfaceStats("S\t100\t10\t2\t3\t200\t20\t4\t5")
	if !ok {
		t.Fatal("counter line was rejected")
	}
	if stats.InboundBytes != 100 || stats.InboundPackets != 10 || stats.InboundErrors != 2 || stats.InboundDrops != 3 || stats.OutboundBytes != 200 || stats.OutboundPackets != 20 || stats.OutboundErrors != 4 || stats.OutboundDrops != 5 {
		t.Fatalf("unexpected parsed counters: %+v", stats)
	}
	if _, ok := parseNetworkInterfaceStats("S\tbroken"); ok {
		t.Fatal("malformed counter line was accepted")
	}
}

// TestParseTcpdumpPacketExtractsDirectionalFields verifies common IPv4 and IPv6 summaries.
func TestParseTcpdumpPacketExtractsDirectionalFields(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		source      string
		destination string
		protocol    string
		sourcePort  int
		destPort    int
		payload     uint64
	}{
		{name: "tcp", line: "176123.1 IP 10.0.0.2.8080 > 10.0.0.3.42122: Flags [P.], length 42", source: "10.0.0.2", destination: "10.0.0.3", protocol: "TCP", sourcePort: 8080, destPort: 42122, payload: 42},
		{name: "udp ipv6", line: "176123.2 IP6 2001:db8::2.53 > 2001:db8::3.52000: UDP, length 18", source: "2001:db8::2", destination: "2001:db8::3", protocol: "UDP", sourcePort: 53, destPort: 52000, payload: 18},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet, ok := parseTcpdumpPacket(test.line)
			if !ok {
				t.Fatal("tcpdump line was rejected")
			}
			if packet.Source != test.source || packet.Destination != test.destination || packet.Protocol != test.protocol || packet.SourcePort != test.sourcePort || packet.DestinationPort != test.destPort || packet.PayloadBytes != test.payload {
				t.Fatalf("unexpected packet: %+v", packet)
			}
		})
	}
	if _, ok := parseTcpdumpPacket("not a packet"); ok {
		t.Fatal("malformed tcpdump line was accepted")
	}
}

// TestNetworkCaptureAggregatorComputesRatesAndBoundsFlows verifies bounded live reduction.
func TestNetworkCaptureAggregatorComputesRatesAndBoundsFlows(t *testing.T) {
	aggregator := newNetworkCaptureAggregator()
	if events := aggregator.consume(networkCaptureLine{Source: "stdout", Text: "S\t100\t10\t0\t0\t200\t20\t0\t0"}); len(events) != 1 || events[0].Sample == nil {
		t.Fatalf("initial counter sample was not emitted: %+v", events)
	}
	aggregator.previousAt = time.Now().Add(-time.Second)
	events := aggregator.consume(networkCaptureLine{Source: "stdout", Text: "S\t300\t30\t1\t2\t500\t50\t3\t4"})
	if len(events) != 1 || events[0].Sample == nil || events[0].Sample.InboundBytesPerSecond < 190 || events[0].Sample.OutboundBytesPerSecond < 290 || events[0].Sample.InboundBytesTotal != 200 || events[0].Sample.OutboundBytesTotal != 300 {
		t.Fatalf("counter rates were not calculated: %+v", events)
	}
	aggregator.consume(networkCaptureLine{Source: "stdout", Text: "P\t176123.2 IP 10.0.0.2.8080 > 10.0.0.3.42122: Flags [P.], length 42"})
	peers := aggregator.peers()
	if len(peers) != 1 || peers[0].Packets != 1 || peers[0].PayloadBytes != 42 {
		t.Fatalf("unexpected peer summary: %+v", peers)
	}
	for index := 0; index < networkCaptureFlowCap+10; index++ {
		line := fmt.Sprintf("P\t176123.2 IP 10.0.0.2.%d > 10.0.0.3.42122: UDP, length 1", 10000+index)
		aggregator.consume(networkCaptureLine{Source: "stdout", Text: line})
	}
	if len(aggregator.flows) > networkCaptureFlowCap {
		t.Fatalf("flow bound exceeded: %d", len(aggregator.flows))
	}
}

// TestTcpdumpCaptureFailureIgnoresStartupNotice verifies normal tcpdump stderr is not shown as a failure.
func TestTcpdumpCaptureFailureIgnoresStartupNotice(t *testing.T) {
	aggregator := newNetworkCaptureAggregator()
	if events := aggregator.consume(networkCaptureLine{Source: "stdout", Text: "P\ttcpdump: listening on any, link-type LINUX_SLL2"}); len(events) != 0 || aggregator.hasError {
		t.Fatalf("normal tcpdump startup notice was classified as an error: events=%+v error=%v", events, aggregator.hasError)
	}
	if events := aggregator.consume(networkCaptureLine{Source: "stdout", Text: "P\ttcpdump: any: You don't have permission to capture on that device"}); len(events) != 1 || !aggregator.hasError {
		t.Fatalf("tcpdump permission failure was not classified: events=%+v error=%v", events, aggregator.hasError)
	}
}

// TestHandleNetworkPrepareRequiresWriteMode verifies the capture setup mutation guard.
func TestHandleNetworkPrepareRequiresWriteMode(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/pod/network/prepare?namespace=default&pod=api", nil)
	response := httptest.NewRecorder()
	server.handleNetworkPrepare(response, request)
	if response.Code != 403 || !strings.Contains(response.Body.String(), "write mode") {
		t.Fatalf("expected write-mode rejection, got %d %s", response.Code, response.Body.String())
	}
}

// TestWriteErrorPreservesNetworkStartupGuidance verifies setup failures are actionable without exposing native details.
func TestWriteErrorPreservesNetworkStartupGuidance(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, errString("the network debug container did not become ready before the startup wait expired; check image pull, Pod Security, and runtime events"))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "check image pull") {
		t.Fatalf("network startup guidance was hidden: %d %s", response.Code, response.Body.String())
	}
}
