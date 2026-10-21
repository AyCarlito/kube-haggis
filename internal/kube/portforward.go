package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	// minimumPortNumber is the first usable TCP port for a port-forward session.
	minimumPortNumber = 1
	// maximumPortNumber is the largest TCP port representable by Kubernetes port forwarding.
	maximumPortNumber = 65535
)

// ForwardPodPort opens a native Kubernetes port-forward session for one Pod port.
func (s *Service) ForwardPodPort(ctx context.Context, contextName, namespace, pod string, targetPort, localPort int, stopChan <-chan struct{}, readyChan chan struct{}) error {
	namespace = strings.TrimSpace(namespace)
	pod = strings.TrimSpace(pod)
	if err := validatePodPortForward(namespace, pod, targetPort, localPort); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	_, _, restConfig, err := s.clients(contextName)
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create typed Kubernetes client: %w", err)
	}

	request := clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("portforward")
	requestURL := request.URL()
	query := requestURL.Query()
	query.Set("port", strconv.Itoa(targetPort))
	requestURL.RawQuery = query.Encode()

	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return fmt.Errorf("create Pod port-forward transport: %w", err)
	}
	spdyDialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, requestURL)
	dialer := spdyDialer
	if websocketDialer, websocketErr := portforward.NewSPDYOverWebsocketDialer(requestURL, restConfig); websocketErr == nil {
		dialer = portforward.NewFallbackDialer(websocketDialer, spdyDialer, func(dialErr error) bool {
			return httpstream.IsUpgradeFailure(dialErr) || httpstream.IsHTTPSProxyError(dialErr)
		})
	}

	forwardStop := make(chan struct{})
	watchStop := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			close(forwardStop)
		case <-stopChan:
			close(forwardStop)
		case <-watchStop:
		}
	}()
	defer func() {
		close(watchStop)
		<-watchDone
	}()

	forwarder, err := portforward.NewOnAddresses(
		dialer,
		[]string{"127.0.0.1"},
		[]string{fmt.Sprintf("%d:%d", localPort, targetPort)},
		forwardStop,
		readyChan,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return fmt.Errorf("create Pod port-forwarder: %w", err)
	}
	return forwarder.ForwardPorts()
}

// validatePodPortForward checks the user-controlled coordinates before loading a cluster client.
func validatePodPortForward(namespace, pod string, targetPort, localPort int) error {
	if namespace == "" || namespace == "all" {
		return fmt.Errorf("namespace is required for Pod port forwarding")
	}
	if pod == "" {
		return fmt.Errorf("Pod name is required for port forwarding")
	}
	if targetPort < minimumPortNumber || targetPort > maximumPortNumber {
		return fmt.Errorf("target port must be between %d and %d", minimumPortNumber, maximumPortNumber)
	}
	if localPort < minimumPortNumber || localPort > maximumPortNumber {
		return fmt.Errorf("local port must be between %d and %d", minimumPortNumber, maximumPortNumber)
	}
	return nil
}
