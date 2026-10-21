package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// extractIngressReadPack summarizes an Ingress and its bounded Service and EndpointSlice view.
func extractIngressReadPack(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	data := IngressReadPackData{
		TLSHosts:       []string{},
		Rules:          []IngressRuleSummary{},
		Backends:       []IngressBackendSummary{},
		Services:       []OperationalReadPackReference{},
		EndpointSlices: []EndpointSliceSummary{},
	}
	partial := false
	issueMessage := ""
	truncated := false
	aggregate := newReadPackAggregateBudget(readPackResultLimit)
	classField := readPackString(object.Object, "spec", "ingressClassName")
	data.IngressClass = classField.Value
	partial = partial || classField.Malformed

	serviceNames := map[string]struct{}{}
	backendKeys := map[string]struct{}{}
	rulesField := readPackSlice(object.Object, "spec", "rules")
	partial = partial || rulesField.Malformed
	if rulesField.Found {
		for index, raw := range rulesField.Value {
			if index >= readPackRuleLimit {
				partial = true
				break
			}
			if !aggregate.take() {
				partial = true
				truncated = true
				break
			}
			rule, ok := raw.(map[string]interface{})
			if !ok {
				partial = true
				continue
			}
			summary, ruleBackends, malformed := ingressRuleSummary(rule)
			paths := make([]IngressPathSummary, 0, len(summary.Paths))
			for _, pathSummary := range summary.Paths {
				if !aggregate.take() {
					partial = true
					truncated = true
					break
				}
				paths = append(paths, pathSummary)
			}
			summary.Paths = paths
			data.Rules = append(data.Rules, summary)
			partial = partial || malformed
			for _, backend := range ruleBackends {
				if addIngressBackend(&data, backend, backendKeys, serviceNames, aggregate) {
					partial = true
					truncated = true
				}
				if aggregate.exhausted() {
					break
				}
			}
			if aggregate.exhausted() {
				partial = true
				truncated = true
				break
			}
		}
	}
	defaultBackend, found, malformed := ingressBackendSummary(object.Object, "spec", "defaultBackend")
	partial = partial || malformed
	if found {
		if addIngressBackend(&data, defaultBackend, backendKeys, serviceNames, aggregate) {
			partial = true
			truncated = true
		}
	}

	tlsField := readPackSlice(object.Object, "spec", "tls")
	partial = partial || tlsField.Malformed
	if tlsField.Found && !aggregate.exhausted() {
		for index, raw := range tlsField.Value {
			if index >= readPackFieldLimit {
				partial = true
				break
			}
			entry, ok := raw.(map[string]interface{})
			if !ok {
				partial = true
				continue
			}
			hosts, _, malformed := readPackStringList(entry, "hosts")
			partial = partial || malformed
			for _, host := range hosts {
				if !aggregate.take() {
					partial = true
					truncated = true
					break
				}
				data.TLSHosts = append(data.TLSHosts, host)
			}
			if aggregate.exhausted() {
				partial = true
				truncated = true
				break
			}
		}
	} else if tlsField.Found {
		partial = true
		truncated = true
	}

	serviceDefinition, serviceCapability := operationalDefinitionForVerb(definitions, "", "v1", "services", true, "list")
	endpointDefinition, endpointCapability := operationalDefinitionForVersions(definitions, "discovery.k8s.io", "endpointslices", true, "list", "v1", "v1beta1")
	if len(serviceNames) > 0 {
		orderedServiceNames := make([]string, 0, len(serviceNames))
		for name := range serviceNames {
			orderedServiceNames = append(orderedServiceNames, name)
		}
		sort.Strings(orderedServiceNames)
		if aggregate.exhausted() {
			partial = true
			truncated = true
		} else if serviceCapability != readPackStateAvailable {
			markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(serviceDefinition, "list"))
			for _, name := range orderedServiceNames {
				if !aggregate.take() {
					partial = true
					truncated = true
					break
				}
				data.Services = append(data.Services, referenceForCoordinates(serviceDefinition, object.GetNamespace(), name, readPackStateUnavailable, "The Service API is not available for backend verification."))
			}
		} else {
			services, err := listOperationalResource(ctx, client, budget, serviceDefinition, object.GetNamespace(), "")
			if err != nil {
				markOperationalDependencyIssue(&partial, &issueMessage, err, "")
				for _, name := range orderedServiceNames {
					if !aggregate.take() {
						partial = true
						truncated = true
						break
					}
					data.Services = append(data.Services, referenceForCoordinates(serviceDefinition, object.GetNamespace(), name, operationalReadPackErrorState(err), operationalReadPackErrorMessage(err)))
				}
			} else {
				if services.Truncated {
					partial = true
					truncated = true
				}
				for _, name := range orderedServiceNames {
					if !aggregate.take() {
						partial = true
						truncated = true
						break
					}
					service := findNamedObject(services.Items, name)
					if service == nil {
						data.Services = append(data.Services, referenceForCoordinates(serviceDefinition, object.GetNamespace(), name, "unresolved", "The backend Service was not returned by the bounded list."))
						continue
					}
					data.Services = append(data.Services, referenceFromObject(service, serviceDefinition, "resolved", ""))
				}
			}
		}

		if aggregate.exhausted() {
			partial = true
			truncated = true
		} else if endpointCapability != readPackStateAvailable {
			markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(endpointDefinition, "list"))
		} else {
			endpointSlices, err := listOperationalResource(ctx, client, budget, endpointDefinition, object.GetNamespace(), "")
			if err != nil {
				markOperationalDependencyIssue(&partial, &issueMessage, err, "")
			} else {
				if endpointSlices.Truncated {
					partial = true
					truncated = true
				}
				for index := range endpointSlices.Items.Items {
					if index >= readPackResultLimit || aggregate.exhausted() {
						partial = true
						truncated = true
						break
					}
					item := &endpointSlices.Items.Items[index]
					serviceName := readPackString(item.Object, "metadata", "labels", "kubernetes.io/service-name")
					partial = partial || serviceName.Malformed
					if serviceName.Value == "" {
						continue
					}
					if _, wanted := serviceNames[serviceName.Value]; !wanted {
						continue
					}
					summary, summaryPartial, summaryTruncated, emitted := endpointSliceSummaryWithBudget(item, aggregate)
					if !emitted {
						partial = true
						truncated = true
						break
					}
					data.EndpointSlices = append(data.EndpointSlices, summary)
					partial = partial || summaryPartial
					truncated = truncated || summaryTruncated
					if summaryTruncated {
						break
					}
				}
			}
		}
	}

	sort.Slice(data.TLSHosts, func(left, right int) bool { return data.TLSHosts[left] < data.TLSHosts[right] })
	sort.Slice(data.Services, func(left, right int) bool { return data.Services[left].Name < data.Services[right].Name })
	sort.Slice(data.EndpointSlices, func(left, right int) bool { return data.EndpointSlices[left].Name < data.EndpointSlices[right].Name })
	empty := len(data.Rules) == 0 && len(data.Backends) == 0 && len(data.TLSHosts) == 0
	return finalizeOperationalPanelWithMessage(readPackPanelIngress, operationalPanelTitle(definition), &data, partial, empty, truncated, "No ingress rules, TLS hosts, or service backends were returned.", issueMessage)
}

// addIngressBackend inserts one unique backend summary and records Service targets for related reads.
func addIngressBackend(data *IngressReadPackData, backend IngressBackendSummary, keys map[string]struct{}, serviceNames map[string]struct{}, aggregate *readPackAggregateBudget) bool {
	key := strings.Join([]string{backend.Kind, backend.Name, backend.Port}, "\x00")
	if backend.Name == "" || key == "\x00\x00" {
		return false
	}
	if _, found := keys[key]; found {
		return false
	}
	if len(data.Backends) >= readPackResultLimit || !aggregate.take() {
		return true
	}
	keys[key] = struct{}{}
	data.Backends = append(data.Backends, backend)
	if backend.Kind == "Service" || backend.Kind == "" {
		serviceNames[backend.Name] = struct{}{}
	}
	return false
}

// ingressRuleSummary parses one Ingress rule into safe paths and backend coordinates.
func ingressRuleSummary(rule map[string]interface{}) (IngressRuleSummary, []IngressBackendSummary, bool) {
	summary := IngressRuleSummary{Paths: []IngressPathSummary{}}
	backends := []IngressBackendSummary{}
	partial := false
	host := readPackString(rule, "host")
	summary.Host = host.Value
	partial = partial || host.Malformed
	httpMap := readPackMap(rule, "http")
	partial = partial || httpMap.Malformed
	if !httpMap.Found {
		return summary, backends, partial
	}
	paths := readPackSlice(httpMap.Value, "paths")
	partial = partial || paths.Malformed
	for index, raw := range paths.Value {
		if index >= readPackRuleLimit {
			partial = true
			break
		}
		pathMap, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		pathSummary, backend, found, malformed := ingressPathSummary(pathMap)
		summary.Paths = append(summary.Paths, pathSummary)
		partial = partial || malformed
		if found {
			backends = append(backends, backend)
		}
	}
	return summary, backends, partial
}

// ingressPathSummary parses one Ingress HTTP path and its v1 backend form.
func ingressPathSummary(pathMap map[string]interface{}) (IngressPathSummary, IngressBackendSummary, bool, bool) {
	pathSummary := IngressPathSummary{}
	backend := IngressBackendSummary{Kind: "Service", State: "unverified"}
	partial := false
	pathField := readPackString(pathMap, "path")
	pathType := readPackString(pathMap, "pathType")
	pathSummary.Path = pathField.Value
	pathSummary.PathType = pathType.Value
	partial = partial || pathField.Malformed || pathType.Malformed
	parsedBackend, found, malformed := ingressBackendSummary(pathMap, "backend")
	partial = partial || malformed
	if !found {
		return pathSummary, backend, false, partial
	}
	pathSummary.ServiceName = parsedBackend.Name
	pathSummary.ServicePort = parsedBackend.Port
	pathSummary.Resource = parsedBackend.Kind
	return pathSummary, parsedBackend, true, partial
}

// ingressBackendSummary parses an Ingress backend in the requested nested field path.
func ingressBackendSummary(object map[string]interface{}, fields ...string) (IngressBackendSummary, bool, bool) {
	backend := IngressBackendSummary{Kind: "Service", State: "unverified"}
	field := readPackMap(object, fields...)
	if field.Malformed || !field.Found {
		return backend, false, field.Malformed
	}
	partial := false
	serviceMap := readPackMap(field.Value, "service")
	partial = partial || serviceMap.Malformed
	if serviceMap.Found {
		name := readPackString(serviceMap.Value, "name")
		partial = partial || name.Malformed
		backend.Name = name.Value
		portMap := readPackMap(serviceMap.Value, "port")
		partial = partial || portMap.Malformed
		if portMap.Found {
			portName := readPackString(portMap.Value, "name")
			portNumber := readPackValueText(portMap.Value, "number")
			partial = partial || portName.Malformed || portNumber.Malformed
			if portName.Value != "" {
				backend.Port = portName.Value
			} else {
				backend.Port = portNumber.Value
			}
		}
		return backend, backend.Name != "", partial
	}
	resourceMap := readPackMap(field.Value, "resource")
	partial = partial || resourceMap.Malformed
	if resourceMap.Found {
		name := readPackString(resourceMap.Value, "name")
		kind := readPackString(resourceMap.Value, "kind")
		partial = partial || name.Malformed || kind.Malformed
		backend.Name = name.Value
		backend.Kind = kind.Value
		return backend, backend.Name != "", partial
	}
	return backend, false, partial
}

// extractEndpointSliceReadPack summarizes an EndpointSlice object without related scans.
func extractEndpointSliceReadPack(definition ResourceDefinition, object *unstructured.Unstructured) OperationalReadPackPanel {
	summary, partial, truncated, _ := endpointSliceSummaryWithBudget(object, newReadPackAggregateBudget(readPackResultLimit))
	data := EndpointSliceReadPackData{Slice: summary}
	return finalizeOperationalPanel(readPackPanelEndpointSlice, operationalPanelTitle(definition), &data, partial, len(summary.Endpoints) == 0, truncated, "No endpoint addresses were reported by this EndpointSlice.")
}

// endpointSliceSummary extracts bounded EndpointSlice addresses, ports, and topology.
func endpointSliceSummary(object *unstructured.Unstructured) (EndpointSliceSummary, bool) {
	summary, partial, _, _ := endpointSliceSummaryWithBudget(object, newReadPackAggregateBudget(readPackResultLimit))
	return summary, partial
}

// endpointSliceSummaryWithBudget extracts one EndpointSlice under a caller-owned aggregate budget.
func endpointSliceSummaryWithBudget(object *unstructured.Unstructured, aggregate *readPackAggregateBudget) (EndpointSliceSummary, bool, bool, bool) {
	summary := EndpointSliceSummary{Name: object.GetName(), Ports: []string{}, Endpoints: []EndpointSummary{}}
	partial := false
	truncated := false
	if !aggregate.take() {
		return summary, true, true, false
	}
	serviceName := readPackString(object.Object, "metadata", "labels", "kubernetes.io/service-name")
	addressType := readPackString(object.Object, "addressType")
	summary.ServiceName = serviceName.Value
	summary.AddressType = addressType.Value
	partial = partial || serviceName.Malformed || addressType.Malformed
	ports := readPackSlice(object.Object, "ports")
	partial = partial || ports.Malformed
	for index, raw := range ports.Value {
		if index >= readPackFieldLimit {
			partial = true
			break
		}
		port, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		name := readPackString(port, "name")
		protocol := readPackString(port, "protocol")
		number := readPackValueText(port, "port")
		partial = partial || name.Malformed || protocol.Malformed || number.Malformed
		label := strings.TrimSpace(strings.Join([]string{protocol.Value, name.Value, number.Value}, " "))
		if len(label) > readPackScalarLimit {
			partial = true
			continue
		}
		if label != "" {
			if !aggregate.take() {
				partial = true
				truncated = true
				break
			}
			summary.Ports = append(summary.Ports, label)
		}
	}
	if aggregate.exhausted() {
		return summary, true, true, true
	}
	endpoints := readPackSlice(object.Object, "endpoints")
	partial = partial || endpoints.Malformed
	for index, raw := range endpoints.Value {
		if index >= readPackResultLimit {
			partial = true
			truncated = true
			break
		}
		if !aggregate.take() {
			partial = true
			truncated = true
			break
		}
		endpoint, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		item := EndpointSummary{Addresses: []string{}}
		addresses, _, malformed := readPackStringList(endpoint, "addresses")
		partial = partial || malformed
		for _, address := range addresses {
			if !aggregate.take() {
				partial = true
				truncated = true
				break
			}
			item.Addresses = append(item.Addresses, address)
		}
		for _, field := range []string{"ready", "serving", "terminating"} {
			value := readPackBool(endpoint, "conditions", field)
			partial = partial || value.Malformed
			if value.Found {
				switch field {
				case "ready":
					item.Ready = readPackBoolPointer(value.Value)
				case "serving":
					item.Serving = readPackBoolPointer(value.Value)
				case "terminating":
					item.Terminating = readPackBoolPointer(value.Value)
				}
			}
		}
		node := readPackString(endpoint, "nodeName")
		zone := readPackString(endpoint, "zone")
		item.NodeName = node.Value
		item.Zone = zone.Value
		partial = partial || node.Malformed || zone.Malformed
		summary.Endpoints = append(summary.Endpoints, item)
		if truncated {
			break
		}
	}
	return summary, partial, truncated, true
}

// extractNetworkPolicyReadPack summarizes selector coverage and bounded affected Pods.
func extractNetworkPolicyReadPack(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	selector, podSelector, selectorValid, selectorPartial := selectorSummary(object.Object, "spec", "podSelector")
	data := NetworkPolicyReadPackData{PodSelector: selector, PolicyTypes: []string{}, IngressRules: []NetworkPolicyRuleSummary{}, EgressRules: []NetworkPolicyRuleSummary{}, AffectedPods: []OperationalReadPackReference{}}
	partial := selectorPartial
	issueMessage := ""
	truncated := false
	aggregate := newReadPackAggregateBudget(readPackResultLimit)
	policyTypes, _, malformed := readPackStringList(object.Object, "spec", "policyTypes")
	partial = partial || malformed
	for _, policyType := range policyTypes {
		if !aggregate.take() {
			partial = true
			truncated = true
			break
		}
		data.PolicyTypes = append(data.PolicyTypes, policyType)
	}
	ingress, ingressPartial, ingressTruncated := networkPolicyRules(object.Object, aggregate, "spec", "ingress")
	data.IngressRules = ingress
	partial = partial || ingressPartial
	truncated = truncated || ingressTruncated
	egress, egressPartial, egressTruncated := networkPolicyRules(object.Object, aggregate, "spec", "egress")
	data.EgressRules = egress
	partial = partial || egressPartial
	truncated = truncated || egressTruncated

	podDefinition, podCapability := operationalDefinitionForVerb(definitions, "", "v1", "pods", true, "list")
	if !selectorValid {
		partial = true
	} else if aggregate.exhausted() {
		partial = true
		truncated = true
	} else if podCapability != readPackStateAvailable {
		markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(podDefinition, "list"))
	} else {
		pods, err := listOperationalResource(ctx, client, budget, podDefinition, object.GetNamespace(), podSelector.String())
		if err != nil {
			markOperationalDependencyIssue(&partial, &issueMessage, err, "")
		} else {
			partial = partial || pods.Truncated
			truncated = truncated || pods.Truncated
			for index := range pods.Items.Items {
				if index >= readPackResultLimit {
					partial = true
					truncated = true
					break
				}
				if !aggregate.take() {
					partial = true
					truncated = true
					break
				}
				data.AffectedPods = append(data.AffectedPods, referenceFromObject(&pods.Items.Items[index], podDefinition, "resolved", ""))
			}
		}
	}

	return finalizeOperationalPanelWithMessage(readPackPanelNetworkPolicy, operationalPanelTitle(definition), &data, partial, false, truncated, "The policy selector returned no affected Pods.", issueMessage)
}

// networkPolicyRules summarizes one ingress or egress rule list.
func networkPolicyRules(object map[string]interface{}, aggregate *readPackAggregateBudget, fields ...string) ([]NetworkPolicyRuleSummary, bool, bool) {
	field := readPackSlice(object, fields...)
	rules := []NetworkPolicyRuleSummary{}
	if field.Malformed {
		return rules, true, false
	}
	partial := false
	truncated := false
	for index, raw := range field.Value {
		if index >= readPackRuleLimit {
			return rules, true, truncated
		}
		if !aggregate.take() {
			return rules, true, true
		}
		rule, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		summary := NetworkPolicyRuleSummary{Peers: []string{}, Ports: []string{}}
		peerField := readPackSlice(rule, "from")
		if fields[len(fields)-1] == "egress" {
			peerField = readPackSlice(rule, "to")
		}
		partial = partial || peerField.Malformed
		for peerIndex, rawPeer := range peerField.Value {
			if peerIndex >= readPackFieldLimit {
				partial = true
				break
			}
			peer, ok := rawPeer.(map[string]interface{})
			if !ok {
				partial = true
				continue
			}
			text, peerPartial := networkPolicyPeerText(peer)
			if text != "" && !aggregate.take() {
				partial = true
				truncated = true
				break
			}
			if text != "" {
				summary.Peers = append(summary.Peers, text)
			}
			partial = partial || peerPartial
		}
		if truncated {
			summary.PeerCount = len(summary.Peers)
			summary.PortCount = len(summary.Ports)
			rules = append(rules, summary)
			return rules, true, true
		}
		portField := readPackSlice(rule, "ports")
		partial = partial || portField.Malformed
		for portIndex, rawPort := range portField.Value {
			if portIndex >= readPackFieldLimit {
				partial = true
				break
			}
			port, ok := rawPort.(map[string]interface{})
			if !ok {
				partial = true
				continue
			}
			protocol := readPackString(port, "protocol")
			portValue := readPackValueText(port, "port")
			endPort := readPackValueText(port, "endPort")
			partial = partial || protocol.Malformed || portValue.Malformed || endPort.Malformed
			text := strings.TrimSpace(strings.Join([]string{protocol.Value, portValue.Value}, " "))
			if endPort.Value != "" {
				text += "-" + endPort.Value
			}
			if text != "" && !aggregate.take() {
				partial = true
				truncated = true
				break
			}
			if text != "" {
				summary.Ports = append(summary.Ports, text)
			}
		}
		summary.PeerCount = len(summary.Peers)
		summary.PortCount = len(summary.Ports)
		rules = append(rules, summary)
		if aggregate.exhausted() {
			partial = true
			truncated = true
			return rules, partial, truncated
		}
	}
	return rules, partial, truncated
}

// networkPolicyPeerText converts one NetworkPolicy peer into a concise safe label.
func networkPolicyPeerText(peer map[string]interface{}) (string, bool) {
	partial := false
	if selector, _, valid, malformed := selectorSummary(peer, "podSelector"); valid {
		if selector.Text == "" {
			return "pod: all", malformed
		}
		return "pod: " + selector.Text, malformed
	} else if malformed {
		partial = true
	}
	if selector, _, valid, malformed := selectorSummary(peer, "namespaceSelector"); valid {
		if selector.Text == "" {
			return "namespace: all", partial || malformed
		}
		return "namespace: " + selector.Text, partial || malformed
	} else if malformed {
		partial = true
	}
	ipBlock := readPackMap(peer, "ipBlock")
	partial = partial || ipBlock.Malformed
	if ipBlock.Found {
		cidr := readPackString(ipBlock.Value, "cidr")
		except, _, exceptMalformed := readPackStringList(ipBlock.Value, "except")
		partial = partial || cidr.Malformed || exceptMalformed
		if cidr.Value != "" {
			return "cidr: " + cidr.Value + fmt.Sprintf(" (%d except)", len(except)), partial
		}
	}
	return "peer", partial
}

// extractHorizontalPodAutoscalerReadPack summarizes HPA target and status fields.
func extractHorizontalPodAutoscalerReadPack(definition ResourceDefinition, object *unstructured.Unstructured) OperationalReadPackPanel {
	data := HorizontalPodAutoscalerReadPackData{Metrics: []AutoscalerMetricSummary{}, Conditions: []OperationalCondition{}}
	partial := false
	targetMap := readPackMap(object.Object, "spec", "scaleTargetRef")
	partial = partial || targetMap.Malformed
	if targetMap.Found {
		apiVersion := readPackString(targetMap.Value, "apiVersion")
		kind := readPackString(targetMap.Value, "kind")
		name := readPackString(targetMap.Value, "name")
		partial = partial || apiVersion.Malformed || kind.Malformed || name.Malformed
		data.Target = OperationalReadPackReference{APIVersion: apiVersion.Value, Kind: kind.Value, Name: name.Value, State: "declared"}
	} else {
		partial = true
		data.Target = OperationalReadPackReference{State: "unresolved", Message: "The HPA target reference was not present in the object."}
	}
	for _, field := range []string{"minReplicas", "maxReplicas"} {
		value := readPackInt64(object.Object, "spec", field)
		partial = partial || value.Malformed
		if value.Found {
			if field == "minReplicas" {
				data.MinReplicas = int64Pointer(value.Value)
			} else {
				data.MaxReplicas = int64Pointer(value.Value)
			}
		}
	}
	for _, field := range []string{"currentReplicas", "desiredReplicas"} {
		value := readPackInt64(object.Object, "status", field)
		partial = partial || value.Malformed
		if value.Found {
			if field == "currentReplicas" {
				data.CurrentReplicas = int64Pointer(value.Value)
			} else {
				data.DesiredReplicas = int64Pointer(value.Value)
			}
		}
	}
	metrics, malformed := autoscalerMetrics(object.Object)
	data.Metrics = metrics
	partial = partial || malformed
	conditions, malformed := readPackConditions(object.Object, "status", "conditions")
	data.Conditions = conditions
	partial = partial || malformed
	return finalizeOperationalPanel(readPackPanelHorizontalScaler, operationalPanelTitle(definition), &data, partial, false, false, "The HPA has no current metric or condition rows.")
}

// autoscalerMetrics combines safe target and current HPA metric readings.
func autoscalerMetrics(object map[string]interface{}) ([]AutoscalerMetricSummary, bool) {
	metrics := []AutoscalerMetricSummary{}
	partial := false
	currentField := readPackSlice(object, "status", "currentMetrics")
	targetField := readPackSlice(object, "spec", "metrics")
	partial = partial || currentField.Malformed || targetField.Malformed
	count := maxInt(len(currentField.Value), len(targetField.Value))
	for index := 0; index < count && index < readPackFieldLimit; index++ {
		metric := AutoscalerMetricSummary{}
		if index < len(currentField.Value) {
			current, ok := currentField.Value[index].(map[string]interface{})
			if !ok {
				partial = true
			} else {
				parsed, malformed := autoscalerMetric(current, false)
				metric = parsed
				partial = partial || malformed
			}
		}
		if index < len(targetField.Value) {
			target, ok := targetField.Value[index].(map[string]interface{})
			if !ok {
				partial = true
			} else {
				parsed, malformed := autoscalerMetric(target, true)
				if metric.Type == "" {
					metric = parsed
				} else {
					metric.Target = parsed.Target
					metric.TargetAverage = parsed.TargetAverage
					metric.TargetUtilization = parsed.TargetUtilization
				}
				partial = partial || malformed
			}
		}
		if metric.Type != "" || metric.Resource != "" || metric.Name != "" {
			metrics = append(metrics, metric)
		}
	}
	if count > readPackFieldLimit {
		partial = true
	}
	return metrics, partial
}

// autoscalerMetric extracts one HPA metric source without copying arbitrary metric objects.
func autoscalerMetric(metric map[string]interface{}, target bool) (AutoscalerMetricSummary, bool) {
	result := AutoscalerMetricSummary{}
	partial := false
	typeField := readPackString(metric, "type")
	result.Type = typeField.Value
	partial = partial || typeField.Malformed
	resourceMap := readPackMap(metric, "resource")
	partial = partial || resourceMap.Malformed
	if resourceMap.Found {
		name := readPackString(resourceMap.Value, "name")
		result.Resource = name.Value
		partial = partial || name.Malformed
		if target {
			result.Target, result.TargetAverage, result.TargetUtilization, partial = metricTarget(resourceMap.Value, partial)
		} else {
			result.Current, partial = metricCurrent(resourceMap.Value, partial)
		}
	}
	externalMap := readPackMap(metric, "external")
	partial = partial || externalMap.Malformed
	if externalMap.Found {
		name := readPackString(externalMap.Value, "metric", "name")
		result.Name = name.Value
		partial = partial || name.Malformed
		if target {
			result.Target, result.TargetAverage, result.TargetUtilization, partial = metricTarget(externalMap.Value, partial)
		} else {
			result.Current, partial = metricCurrent(externalMap.Value, partial)
		}
	}
	return result, partial
}

// metricTarget extracts the target scalar forms supported by HPA v2 metrics.
func metricTarget(object map[string]interface{}, partial bool) (string, string, *int64, bool) {
	targetMap := readPackMap(object, "target")
	partial = partial || targetMap.Malformed
	if !targetMap.Found {
		return "", "", nil, partial
	}
	value := readPackValueText(targetMap.Value, "value")
	average := readPackValueText(targetMap.Value, "averageValue")
	utilization := readPackInt64(targetMap.Value, "averageUtilization")
	partial = partial || value.Malformed || average.Malformed || utilization.Malformed
	return value.Value, average.Value, optionalInt64(utilization), partial
}

// metricCurrent extracts the current scalar forms supported by HPA v2 metrics.
func metricCurrent(object map[string]interface{}, partial bool) (string, bool) {
	value := readPackValueText(object, "current", "value")
	average := readPackValueText(object, "current", "averageValue")
	utilization := readPackInt64(object, "current", "averageUtilization")
	partial = partial || value.Malformed || average.Malformed || utilization.Malformed
	return firstNonEmpty(value.Value, average.Value, optionalInt64Text(utilization)), partial
}

// optionalInt64 returns nil for an absent integer field.
func optionalInt64(field readPackFieldResult[int64]) *int64 {
	if !field.Found || field.Malformed {
		return nil
	}
	return int64Pointer(field.Value)
}

// optionalInt64Text returns an optional integer as display text.
func optionalInt64Text(field readPackFieldResult[int64]) string {
	if !field.Found || field.Malformed {
		return ""
	}
	return fmt.Sprintf("%d%%", field.Value)
}

// extractPodDisruptionBudgetReadPack summarizes PDB allowance and health conditions.
func extractPodDisruptionBudgetReadPack(definition ResourceDefinition, object *unstructured.Unstructured) OperationalReadPackPanel {
	data := PodDisruptionBudgetReadPackData{Conditions: []OperationalCondition{}}
	partial := false
	for _, field := range []string{"minAvailable", "maxUnavailable"} {
		value := readPackValueText(object.Object, "spec", field)
		partial = partial || value.Malformed
		if field == "minAvailable" {
			data.MinAvailable = value.Value
		} else {
			data.MaxUnavailable = value.Value
		}
	}
	for _, field := range []string{"expectedPods", "currentHealthy", "desiredHealthy", "disruptionsAllowed"} {
		value := readPackInt64(object.Object, "status", field)
		partial = partial || value.Malformed
		if !value.Found {
			continue
		}
		switch field {
		case "expectedPods":
			data.ExpectedPods = int64Pointer(value.Value)
		case "currentHealthy":
			data.CurrentHealthy = int64Pointer(value.Value)
		case "desiredHealthy":
			data.DesiredHealthy = int64Pointer(value.Value)
		case "disruptionsAllowed":
			data.DisruptionsAllowed = int64Pointer(value.Value)
		}
	}
	conditions, malformed := readPackConditions(object.Object, "status", "conditions")
	data.Conditions = conditions
	partial = partial || malformed
	return finalizeOperationalPanel(readPackPanelDisruptionBudget, operationalPanelTitle(definition), &data, partial, false, false, "The PDB has no status counters yet; disruption allowance is not reported.")
}

// extractResourceQuotaReadPack summarizes hard and used quota values.
func extractResourceQuotaReadPack(definition ResourceDefinition, object *unstructured.Unstructured) OperationalReadPackPanel {
	data := ResourceQuotaReadPackData{Scopes: []string{}, Usage: []QuotaUsageSummary{}}
	partial := false
	scopes, _, malformed := readPackStringList(object.Object, "spec", "scopes")
	data.Scopes = scopes
	partial = partial || malformed
	hard, hardFound, hardPartial := readPackQuantityMap(object.Object, "spec", "hard")
	used, usedFound, usedPartial := readPackQuantityMap(object.Object, "status", "used")
	partial = partial || hardPartial || usedPartial
	keys := map[string]struct{}{}
	for key := range hard {
		keys[key] = struct{}{}
	}
	for key := range used {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for index, key := range ordered {
		if index >= readPackResultLimit {
			partial = true
			break
		}
		item := QuotaUsageSummary{Resource: key, Used: used[key], Hard: hard[key]}
		if item.Used != "" && item.Hard != "" {
			item.Percent = parseQuantityPercentage(item.Used, item.Hard)
		}
		data.Usage = append(data.Usage, item)
	}
	empty := !hardFound && !usedFound
	return finalizeOperationalPanel(readPackPanelResourceQuota, operationalPanelTitle(definition), &data, partial, empty, false, "No hard or used quota quantities were returned.")
}

// extractLimitRangeReadPack summarizes every bounded LimitRange item.
func extractLimitRangeReadPack(definition ResourceDefinition, object *unstructured.Unstructured) OperationalReadPackPanel {
	data := LimitRangeReadPackData{Items: []LimitRangeItemSummary{}}
	field := readPackSlice(object.Object, "spec", "limits")
	partial := field.Malformed
	for index, raw := range field.Value {
		if index >= readPackResultLimit {
			partial = true
			break
		}
		item, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		summary := LimitRangeItemSummary{Default: map[string]string{}, DefaultRequest: map[string]string{}, Max: map[string]string{}, Min: map[string]string{}}
		typeField := readPackString(item, "type")
		summary.Type = typeField.Value
		partial = partial || typeField.Malformed
		for fieldName, destination := range map[string]*map[string]string{"default": &summary.Default, "defaultRequest": &summary.DefaultRequest, "max": &summary.Max, "min": &summary.Min} {
			values, _, malformed := readPackQuantityMap(item, fieldName)
			*destination = values
			partial = partial || malformed
		}
		data.Items = append(data.Items, summary)
	}
	return finalizeOperationalPanel(readPackPanelLimitRange, operationalPanelTitle(definition), &data, partial, len(data.Items) == 0, false, "No LimitRange item defaults or bounds were returned.")
}

// extractPersistentVolumeReadPack summarizes PV lifecycle fields and verifies safe claim and class links.
func extractPersistentVolumeReadPack(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	data := PersistentVolumeReadPackData{Capacity: map[string]string{}, Relationships: []OperationalReadPackReference{}}
	partial := false
	issueMessage := ""
	phase := readPackString(object.Object, "status", "phase")
	reason := readPackString(object.Object, "status", "reason")
	storageClass := readPackString(object.Object, "spec", "storageClassName")
	reclaim := readPackString(object.Object, "spec", "persistentVolumeReclaimPolicy")
	volumeMode := readPackString(object.Object, "spec", "volumeMode")
	data.Phase = phase.Value
	data.Reason = reason.Value
	data.StorageClass = storageClass.Value
	data.ReclaimPolicy = reclaim.Value
	data.VolumeMode = volumeMode.Value
	partial = partial || phase.Malformed || reason.Malformed || storageClass.Malformed || reclaim.Malformed || volumeMode.Malformed
	capacity, _, malformed := readPackQuantityMap(object.Object, "spec", "capacity")
	data.Capacity = capacity
	partial = partial || malformed
	claimRef := readPackMap(object.Object, "spec", "claimRef")
	partial = partial || claimRef.Malformed
	claimName := ""
	claimNamespace := ""
	if claimRef.Found {
		name := readPackString(claimRef.Value, "name")
		namespace := readPackString(claimRef.Value, "namespace")
		uid := readPackString(claimRef.Value, "uid")
		claimName = name.Value
		claimNamespace = namespace.Value
		data.ClaimRef = OperationalReadPackReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Name: claimName, Namespace: claimNamespace, State: "declared"}
		partial = partial || name.Malformed || namespace.Malformed || uid.Malformed
	}

	providerMessage := readPackString(object.Object, "status", "message")
	data.ProviderMessage = providerMessage.Found
	partial = partial || providerMessage.Malformed
	nodeAffinity := readPackMap(object.Object, "spec", "nodeAffinity")
	data.NodeAffinity = nodeAffinity.Found
	partial = partial || nodeAffinity.Malformed
	source := persistentVolumeSource(object.Object)
	data.Source = source
	if source == "" {
		partial = true
	}

	if claimName != "" && claimNamespace != "" {
		claimDefinition, claimCapability := operationalDefinitionForVerb(definitions, "", "v1", "persistentvolumeclaims", true, "get")
		if claimCapability != readPackStateAvailable {
			markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(claimDefinition, "get"))
			data.Relationships = append(data.Relationships, referenceForCoordinates(claimDefinition, claimNamespace, claimName, readPackStateUnavailable, "The PVC API is not available for binding verification."))
		} else {
			claim, err := getOperationalResource(ctx, client, budget, claimDefinition, claimNamespace, claimName)
			if err != nil {
				markOperationalDependencyIssue(&partial, &issueMessage, err, "")
				data.Relationships = append(data.Relationships, referenceForCoordinates(claimDefinition, claimNamespace, claimName, operationalReadPackErrorState(err), operationalReadPackErrorMessage(err)))
			} else {
				data.Relationships = append(data.Relationships, referenceFromObject(claim, claimDefinition, "resolved", ""))
			}
		}
	}
	if storageClass.Value != "" {
		classDefinition, classCapability := operationalDefinitionForVerb(definitions, "storage.k8s.io", "v1", "storageclasses", false, "get")
		if classCapability != readPackStateAvailable {
			markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(classDefinition, "get"))
			data.Relationships = append(data.Relationships, referenceForCoordinates(classDefinition, "", storageClass.Value, readPackStateUnavailable, "The StorageClass API is not available for lifecycle verification."))
		} else {
			storageClassObject, err := getOperationalResource(ctx, client, budget, classDefinition, "", storageClass.Value)
			if err != nil {
				markOperationalDependencyIssue(&partial, &issueMessage, err, "")
				data.Relationships = append(data.Relationships, referenceForCoordinates(classDefinition, "", storageClass.Value, operationalReadPackErrorState(err), operationalReadPackErrorMessage(err)))
			} else {
				data.Relationships = append(data.Relationships, referenceFromObject(storageClassObject, classDefinition, "resolved", ""))
			}
		}
	}
	empty := data.Phase == "" && data.StorageClass == "" && data.Source == "" && len(data.Capacity) == 0
	return finalizeOperationalPanelWithMessage(readPackPanelPersistentVolume, operationalPanelTitle(definition), &data, partial, empty, false, "No PV lifecycle fields were returned.", issueMessage)
}

// persistentVolumeSource identifies the first known non-secret PV source field.
func persistentVolumeSource(object map[string]interface{}) string {
	spec, found, _ := unstructured.NestedMap(object, "spec")
	if !found {
		return ""
	}
	known := []string{"csi", "cinder", "fc", "flexVolume", "gcePersistentDisk", "glusterfs", "hostPath", "iscsi", "local", "nfs", "photonPersistentDisk", "portworxVolume", "quobyte", "rbd", "scaleIO", "vsphereVolume"}
	for _, field := range known {
		if _, found := spec[field]; found {
			return field
		}
	}
	return ""
}

// extractStorageClassReadPack summarizes provisioning and bounded PV relationships.
func extractStorageClassReadPack(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	data := StorageClassReadPackData{MountOptions: []string{}, PersistentVolumes: []OperationalReadPackReference{}}
	partial := false
	issueMessage := ""
	truncated := false
	provisioner := readPackString(object.Object, "provisioner")
	reclaim := readPackString(object.Object, "reclaimPolicy")
	binding := readPackString(object.Object, "volumeBindingMode")
	data.Provisioner = provisioner.Value
	data.ReclaimPolicy = reclaim.Value
	data.VolumeBindingMode = binding.Value
	partial = partial || provisioner.Malformed || reclaim.Malformed || binding.Malformed
	expansion := readPackBool(object.Object, "allowVolumeExpansion")
	data.AllowVolumeExpansion = optionalBool(expansion)
	partial = partial || expansion.Malformed
	mountOptions, _, malformed := readPackStringList(object.Object, "mountOptions")
	data.MountOptions = mountOptions
	partial = partial || malformed
	topologies := readPackSlice(object.Object, "allowedTopologies")
	data.AllowedTopologies = len(topologies.Value)
	partial = partial || topologies.Malformed

	pvDefinition, pvCapability := operationalDefinitionForVerb(definitions, "", "v1", "persistentvolumes", false, "list")
	if pvCapability != readPackStateAvailable {
		markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(pvDefinition, "list"))
	} else {
		items, err := listOperationalResource(ctx, client, budget, pvDefinition, "", "")
		if err != nil {
			markOperationalDependencyIssue(&partial, &issueMessage, err, "")
		} else {
			partial = partial || items.Truncated
			truncated = items.Truncated
			for index := range items.Items.Items {
				if index >= readPackResultLimit {
					partial = true
					truncated = true
					break
				}
				item := &items.Items.Items[index]
				class := readPackString(item.Object, "spec", "storageClassName")
				partial = partial || class.Malformed
				if class.Value != object.GetName() {
					continue
				}
				data.PersistentVolumes = append(data.PersistentVolumes, referenceFromObject(item, pvDefinition, "resolved", ""))
			}
		}
	}
	return finalizeOperationalPanelWithMessage(readPackPanelStorageClass, operationalPanelTitle(definition), &data, partial, false, truncated, "No PersistentVolumes use this StorageClass.", issueMessage)
}

// optionalBool returns a pointer only when a generic bool field was present and valid.
func optionalBool(field readPackFieldResult[bool]) *bool {
	if !field.Found || field.Malformed {
		return nil
	}
	return readPackBoolPointer(field.Value)
}

// extractServiceAccountReadPack summarizes safe references and read-only RBAC bindings.
func extractServiceAccountReadPack(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	data := ServiceAccountReadPackData{Secrets: []string{}, ImagePullSecrets: []string{}, RoleBindings: []RBACBindingSummary{}, ClusterRoleBindings: []RBACBindingSummary{}}
	partial := false
	issueMessage := ""
	truncated := false
	automount := readPackBool(object.Object, "automountServiceAccountToken")
	data.AutomountServiceAccountToken = optionalBool(automount)
	partial = partial || automount.Malformed
	data.Secrets, partial = serviceAccountReferenceNames(object.Object, "secrets", partial)
	data.ImagePullSecrets, partial = serviceAccountReferenceNames(object.Object, "imagePullSecrets", partial)

	roleBindingDefinition, roleBindingCapability := operationalDefinitionForVerb(definitions, "rbac.authorization.k8s.io", "v1", "rolebindings", true, "list")
	if roleBindingCapability != readPackStateAvailable {
		markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(roleBindingDefinition, "list"))
	} else {
		items, err := listOperationalResource(ctx, client, budget, roleBindingDefinition, object.GetNamespace(), "")
		if err != nil {
			markOperationalDependencyIssue(&partial, &issueMessage, err, "")
		} else {
			partial = partial || items.Truncated
			truncated = truncated || items.Truncated
			data.RoleBindings, partial = matchingRBACBindings(items.Items, roleBindingDefinition, object.GetNamespace(), object.GetName(), partial)
		}
	}
	clusterBindingDefinition, clusterBindingCapability := operationalDefinitionForVerb(definitions, "rbac.authorization.k8s.io", "v1", "clusterrolebindings", false, "list")
	if clusterBindingCapability != readPackStateAvailable {
		markOperationalDependencyIssue(&partial, &issueMessage, nil, operationalRelatedMessage(clusterBindingDefinition, "list"))
	} else {
		items, err := listOperationalResource(ctx, client, budget, clusterBindingDefinition, "", "")
		if err != nil {
			markOperationalDependencyIssue(&partial, &issueMessage, err, "")
		} else {
			partial = partial || items.Truncated
			truncated = truncated || items.Truncated
			data.ClusterRoleBindings, partial = matchingRBACBindings(items.Items, clusterBindingDefinition, object.GetNamespace(), object.GetName(), partial)
		}
	}
	return finalizeOperationalPanelWithMessage(readPackPanelServiceAccount, operationalPanelTitle(definition), &data, partial, false, truncated, "No read-only RoleBinding or ClusterRoleBinding references this ServiceAccount.", issueMessage)
}

// serviceAccountReferenceNames reads only named Secret and image-pull references from a ServiceAccount.
func serviceAccountReferenceNames(object map[string]interface{}, field string, partial bool) ([]string, bool) {
	entries := readPackSlice(object, field)
	partial = partial || entries.Malformed
	result := []string{}
	for index, raw := range entries.Value {
		if index >= readPackFieldLimit {
			return result, true
		}
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return result, true
		}
		name := readPackString(entry, "name")
		partial = partial || name.Malformed
		if name.Value != "" {
			result = append(result, name.Value)
		}
	}
	return result, partial
}

// matchingRBACBindings filters bounded RoleBinding objects to one ServiceAccount subject.
func matchingRBACBindings(items *unstructured.UnstructuredList, definition ResourceDefinition, namespace, serviceAccount string, partial bool) ([]RBACBindingSummary, bool) {
	result := []RBACBindingSummary{}
	if items == nil {
		return result, partial
	}
	for index := range items.Items {
		if index >= readPackBindingLimit {
			return result, true
		}
		item := &items.Items[index]
		summary, matched, malformed := rbacBindingSummary(item, definition, namespace, serviceAccount)
		partial = partial || malformed
		if matched {
			result = append(result, summary)
		}
	}
	return result, partial
}

// rbacBindingSummary returns one binding only when a ServiceAccount subject matches exactly.
func rbacBindingSummary(object *unstructured.Unstructured, definition ResourceDefinition, namespace, serviceAccount string) (RBACBindingSummary, bool, bool) {
	summary := RBACBindingSummary{Name: object.GetName(), Namespace: object.GetNamespace(), Subjects: []RBACSubjectSummary{}}
	partial := false
	roleRef := readPackMap(object.Object, "roleRef")
	partial = partial || roleRef.Malformed
	if roleRef.Found {
		kind := readPackString(roleRef.Value, "kind")
		name := readPackString(roleRef.Value, "name")
		summary.RoleKind = kind.Value
		summary.RoleName = name.Value
		partial = partial || kind.Malformed || name.Malformed
	}
	subjects := readPackSlice(object.Object, "subjects")
	partial = partial || subjects.Malformed
	matched := false
	for index, raw := range subjects.Value {
		if index >= readPackFieldLimit {
			partial = true
			break
		}
		subject, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		kind := readPackString(subject, "kind")
		name := readPackString(subject, "name")
		subjectNamespace := readPackString(subject, "namespace")
		partial = partial || kind.Malformed || name.Malformed || subjectNamespace.Malformed
		if kind.Value == "ServiceAccount" && name.Value == serviceAccount && (subjectNamespace.Value == "" || subjectNamespace.Value == namespace) {
			matched = true
		}
		if kind.Value != "" && name.Value != "" {
			summary.Subjects = append(summary.Subjects, RBACSubjectSummary{Kind: kind.Value, Name: name.Value, Namespace: subjectNamespace.Value})
		}
	}
	_ = definition
	return summary, matched, partial
}

// extractPodReadPack summarizes scheduling, status, probes, volumes, and security posture.
func extractPodReadPack(definition ResourceDefinition, object *unstructured.Unstructured) OperationalReadPackPanel {
	data := PodReadPackData{Conditions: []OperationalCondition{}, ReadinessGates: []string{}, Containers: []PodContainerSummary{}, InitContainers: []PodContainerSummary{}, EphemeralContainers: []PodContainerSummary{}, Volumes: []PodVolumeSummary{}}
	partial := false
	for field, destination := range map[string]*string{"phase": &data.Phase, "reason": &data.Reason} {
		value := readPackString(object.Object, "status", field)
		*destination = value.Value
		partial = partial || value.Malformed
	}
	for field, destination := range map[string]*string{"nodeName": &data.NodeName, "schedulerName": &data.SchedulerName} {
		value := readPackString(object.Object, "spec", field)
		*destination = value.Value
		partial = partial || value.Malformed
	}
	podIP := readPackString(object.Object, "status", "podIP")
	data.PodIP = podIP.Value
	partial = partial || podIP.Malformed
	conditions, malformed := readPackConditions(object.Object, "status", "conditions")
	data.Conditions = conditions
	partial = partial || malformed
	gates, _, malformed := readPackStringList(object.Object, "spec", "readinessGates")
	data.ReadinessGates = gates
	partial = partial || malformed
	data.Containers, malformed = podContainerSummaries(object.Object, "containers", "containerStatuses")
	partial = partial || malformed
	data.InitContainers, malformed = podContainerSummaries(object.Object, "initContainers", "initContainerStatuses")
	partial = partial || malformed
	data.EphemeralContainers, malformed = podContainerSummaries(object.Object, "ephemeralContainers", "ephemeralContainerStatuses")
	partial = partial || malformed
	data.Volumes, malformed = podVolumeSummaries(object.Object)
	partial = partial || malformed
	data.SecurityContext, malformed = podSecurityContextSummary(object.Object)
	partial = partial || malformed
	return finalizeOperationalPanel(readPackPanelPod, operationalPanelTitle(definition), &data, partial, false, false, "No Pod status or scheduling fields were returned.")
}

// podContainerSummaries merges safe PodSpec containers with status and probe summaries.
func podContainerSummaries(object map[string]interface{}, specField, statusField string) ([]PodContainerSummary, bool) {
	result := []PodContainerSummary{}
	partial := false
	spec := readPackSlice(object, "spec", specField)
	statuses := readPackSlice(object, "status", statusField)
	partial = partial || spec.Malformed || statuses.Malformed
	statusByName := map[string]map[string]interface{}{}
	for index, raw := range statuses.Value {
		if index >= readPackContainerLimit {
			partial = true
			break
		}
		entry, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		name := readPackString(entry, "name")
		partial = partial || name.Malformed
		if name.Value != "" {
			statusByName[name.Value] = entry
		}
	}
	for index, raw := range spec.Value {
		if index >= readPackContainerLimit {
			return result, true
		}
		container, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		summary, malformed := podContainerSummary(container, statusByName[readPackString(container, "name").Value])
		result = append(result, summary)
		partial = partial || malformed
	}
	return result, partial
}

// podContainerSummary extracts one regular, init, or ephemeral container safely.
func podContainerSummary(spec, status map[string]interface{}) (PodContainerSummary, bool) {
	partial := false
	name := readPackString(spec, "name")
	image := readPackString(spec, "image")
	result := PodContainerSummary{Name: name.Value, Image: image.Value, Probes: []PodProbeSummary{}}
	partial = partial || name.Malformed || image.Malformed
	for field, label := range map[string]string{"livenessProbe": "liveness", "readinessProbe": "readiness", "startupProbe": "startup"} {
		probe, found, malformed := podProbeSummary(spec, field, label)
		if found {
			result.Probes = append(result.Probes, probe)
		}
		partial = partial || malformed
	}
	result.SecurityContext, partial = podContainerSecuritySummary(spec, partial)
	if status == nil {
		return result, partial
	}
	ready := readPackBool(status, "ready")
	started := readPackBool(status, "started")
	restarts := readPackInt64(status, "restartCount")
	result.Ready = optionalBool(ready)
	result.Started = optionalBool(started)
	result.RestartCount = optionalInt64(restarts)
	partial = partial || ready.Malformed || started.Malformed || restarts.Malformed
	state, reason, exitCode, statePartial := podContainerState(status, "state")
	result.State = state
	result.StateReason = reason
	result.StateExitCode = exitCode
	partial = partial || statePartial
	lastState, lastReason, lastExitCode, lastPartial := podContainerState(status, "lastState")
	_ = lastState
	result.LastTerminationReason = lastReason
	result.LastTerminationExitCode = lastExitCode
	partial = partial || lastPartial
	return result, partial
}

// podProbeSummary identifies one configured probe handler without returning command arguments.
func podProbeSummary(spec map[string]interface{}, field, label string) (PodProbeSummary, bool, bool) {
	probe := readPackMap(spec, field)
	if probe.Malformed || !probe.Found {
		return PodProbeSummary{Name: label}, false, probe.Malformed
	}
	result := PodProbeSummary{Name: label, Configured: true}
	for _, handler := range []string{"httpGet", "tcpSocket", "grpc", "exec"} {
		if _, found := probe.Value[handler]; found {
			result.Handler = handler
			break
		}
	}
	return result, true, false
}

// podContainerState extracts safe waiting, running, or terminated state details.
func podContainerState(status map[string]interface{}, field string) (string, string, *int64, bool) {
	state := readPackMap(status, field)
	if state.Malformed || !state.Found {
		return "", "", nil, state.Malformed
	}
	for _, candidate := range []string{"waiting", "running", "terminated"} {
		entry := readPackMap(state.Value, candidate)
		if entry.Malformed {
			return candidate, "", nil, true
		}
		if !entry.Found {
			continue
		}
		reason := readPackString(entry.Value, "reason")
		exitCode := readPackInt64(entry.Value, "exitCode")
		return candidate, reason.Value, optionalInt64(exitCode), reason.Malformed || exitCode.Malformed
	}
	return "", "", nil, false
}

// podVolumeSummaries extracts safe volume type and object references.
func podVolumeSummaries(object map[string]interface{}) ([]PodVolumeSummary, bool) {
	volumes := readPackSlice(object, "spec", "volumes")
	result := []PodVolumeSummary{}
	partial := volumes.Malformed
	for index, raw := range volumes.Value {
		if index >= readPackVolumeLimit {
			return result, true
		}
		volume, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		name := readPackString(volume, "name")
		item := PodVolumeSummary{Name: name.Value}
		partial = partial || name.Malformed
		for _, source := range []struct {
			field string
			label string
			name  string
		}{{"configMap", "configMap", "name"}, {"secret", "secret", "secretName"}, {"persistentVolumeClaim", "persistentVolumeClaim", "claimName"}, {"projected", "projected", "sources"}, {"emptyDir", "emptyDir", "medium"}, {"hostPath", "hostPath", "path"}, {"downwardAPI", "downwardAPI", "items"}} {
			value := readPackMap(volume, source.field)
			partial = partial || value.Malformed
			if !value.Found {
				continue
			}
			item.Type = source.label
			if source.field == "projected" || source.field == "downwardAPI" {
				item.Reference = "configured"
			} else {
				reference := readPackValueText(value.Value, source.name)
				item.Reference = reference.Value
				partial = partial || reference.Malformed
			}
			break
		}
		readOnly := readPackBool(volume, "readOnly")
		item.ReadOnly = optionalBool(readOnly)
		partial = partial || readOnly.Malformed
		if item.Type == "" {
			item.Type = "unknown"
			partial = true
		}
		result = append(result, item)
	}
	return result, partial
}

// podSecurityContextSummary extracts non-secret Pod-level security settings.
func podSecurityContextSummary(object map[string]interface{}) (PodSecurityContextSummary, bool) {
	contextMap := readPackMap(object, "spec", "securityContext")
	result := PodSecurityContextSummary{}
	partial := contextMap.Malformed
	if contextMap.Found {
		for field, destination := range map[string]**int64{"runAsUser": &result.RunAsUser, "runAsGroup": &result.RunAsGroup, "fsGroup": &result.FSGroup} {
			value := readPackInt64(contextMap.Value, field)
			*destination = optionalInt64(value)
			partial = partial || value.Malformed
		}
		for field, destination := range map[string]**bool{"runAsNonRoot": &result.RunAsNonRoot} {
			value := readPackBool(contextMap.Value, field)
			*destination = optionalBool(value)
			partial = partial || value.Malformed
		}
		seccomp := readPackMap(contextMap.Value, "seccompProfile")
		if seccomp.Found {
			profile := readPackString(seccomp.Value, "type")
			result.SeccompProfileType = profile.Value
			partial = partial || profile.Malformed
		}
		partial = partial || seccomp.Malformed
	}
	for field, destination := range map[string]**bool{"hostNetwork": &result.HostNetwork, "hostPID": &result.HostPID, "hostIPC": &result.HostIPC} {
		value := readPackBool(object, "spec", field)
		*destination = optionalBool(value)
		partial = partial || value.Malformed
	}
	return result, partial
}

// podContainerSecuritySummary extracts non-secret container-level security settings.
func podContainerSecuritySummary(spec map[string]interface{}, partial bool) (PodContainerSecuritySummary, bool) {
	result := PodContainerSecuritySummary{Capabilities: []string{}}
	contextMap := readPackMap(spec, "securityContext")
	if contextMap.Malformed || !contextMap.Found {
		return result, partial || contextMap.Malformed
	}
	for field, destination := range map[string]**bool{"privileged": &result.Privileged, "allowPrivilegeEscalation": &result.AllowPrivilegeEscalation, "readOnlyRootFilesystem": &result.ReadOnlyRootFilesystem, "runAsNonRoot": &result.RunAsNonRoot} {
		value := readPackBool(contextMap.Value, field)
		*destination = optionalBool(value)
		partial = partial || value.Malformed
	}
	user := readPackInt64(contextMap.Value, "runAsUser")
	result.RunAsUser = optionalInt64(user)
	partial = partial || user.Malformed
	seccomp := readPackMap(contextMap.Value, "seccompProfile")
	if seccomp.Found {
		profile := readPackString(seccomp.Value, "type")
		result.SeccompProfileType = profile.Value
		partial = partial || profile.Malformed
	}
	partial = partial || seccomp.Malformed
	for _, field := range []string{"add", "drop"} {
		values, _, malformed := readPackStringList(contextMap.Value, "capabilities", field)
		result.Capabilities = append(result.Capabilities, values...)
		partial = partial || malformed
	}
	return result, partial
}

// markOperationalDependencyIssue records a safe partial dependency message.
func markOperationalDependencyIssue(partial *bool, issueMessage *string, err error, message string) {
	*partial = true
	if *issueMessage != "" {
		return
	}
	if message != "" {
		*issueMessage = message
		return
	}
	if err != nil {
		*issueMessage = operationalReadPackErrorMessage(err)
	}
}

// finalizeOperationalPanel creates the final explicit state for one extracted panel.
func finalizeOperationalPanel(key, title string, data interface{}, partial, empty, truncated bool, emptyMessage string) OperationalReadPackPanel {
	return finalizeOperationalPanelWithMessage(key, title, data, partial, empty, truncated, emptyMessage, "")
}

// finalizeOperationalPanelWithMessage creates a panel while retaining a safe dependency limitation.
func finalizeOperationalPanelWithMessage(key, title string, data interface{}, partial, empty, truncated bool, emptyMessage, issueMessage string) OperationalReadPackPanel {
	state := readPackStateAvailable
	message := "Structured read-only data is available."
	if partial || truncated {
		state = readPackStatePartial
		message = issueMessage
		if message == "" {
			message = operationalMalformedMessage()
		}
	} else if empty {
		state = readPackStateEmpty
		message = emptyMessage
	}
	return newOperationalReadPackPanel(key, title, state, message, data, truncated)
}

// findNamedObject returns one list item by metadata.name without exposing its body.
func findNamedObject(items *unstructured.UnstructuredList, name string) *unstructured.Unstructured {
	if items == nil {
		return nil
	}
	for index := range items.Items {
		if items.Items[index].GetName() == name {
			return &items.Items[index]
		}
	}
	return nil
}

// maxInt returns the larger integer for paired metric arrays.
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// firstNonEmpty returns the first non-empty safe scalar.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// readPackValueText reads a string or numeric scalar for safe status display.
func readPackValueText(object map[string]interface{}, fields ...string) readPackFieldResult[string] {
	value, found, err := unstructured.NestedFieldCopy(object, fields...)
	if err != nil {
		return readPackFieldResult[string]{Malformed: true}
	}
	if !found {
		return readPackFieldResult[string]{}
	}
	text := parseNumericString(value)
	if text == "" {
		return readPackFieldResult[string]{Found: true, Malformed: true}
	}
	if len(text) > readPackScalarLimit {
		return readPackFieldResult[string]{Found: true, Malformed: true}
	}
	return readPackFieldResult[string]{Value: text, Found: true}
}
