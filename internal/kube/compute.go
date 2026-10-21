package kube

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const (
	computeQuantityStateAvailable     = "available"
	computeQuantityStateAbsent        = "absent"
	computeQuantityStatePartial       = "partial"
	computeQuantityStateNotApplicable = "not-applicable"
)

// extractComputeReadPack reads bounded Node or Pod quantities and one exact metrics sample.
func extractComputeReadPack(ctx context.Context, client dynamic.Interface, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	return extractComputeReadPackAt(ctx, client, definition, object, budget, time.Now().UTC())
}

// extractComputeReadPackAt extracts a Compute panel against a caller-supplied clock for deterministic freshness tests.
func extractComputeReadPackAt(ctx context.Context, client dynamic.Interface, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget, now time.Time) OperationalReadPackPanel {
	data, objectPartial := computeReadPackObjectData(definition, object)
	data.Usage = readComputeUsage(ctx, client, definition, object, budget, now)
	alignComputeUsageUnits(&data)
	state := computeReadPackState(data, objectPartial)
	return newOperationalReadPackPanel(readPackPanelCompute, "Compute", state, computeReadPackMessage(data, objectPartial), &data, false)
}

// computeReadPackObjectData extracts only CPU and memory fields from one Node or Pod object.
func computeReadPackObjectData(definition ResourceDefinition, object *unstructured.Unstructured) (ComputeReadPackData, bool) {
	data := ComputeReadPackData{Kind: definition.Kind}
	partial := false
	if object == nil {
		data.Requests = notApplicableComputeQuantitySet("The resource object was not available.")
		data.Limits = notApplicableComputeQuantitySet("The resource object was not available.")
		data.Capacity = notApplicableComputeQuantitySet("The resource object was not available.")
		data.Allocatable = notApplicableComputeQuantitySet("The resource object was not available.")
		return data, true
	}

	data.Name = object.GetName()
	data.Namespace = object.GetNamespace()
	switch definition.Kind {
	case "Node":
		data.Requests = notApplicableComputeQuantitySet("Kubernetes does not expose Node-level request values equivalent to Pod requests.")
		data.Limits = notApplicableComputeQuantitySet("Kubernetes does not expose Node-level limit values equivalent to Pod limits.")
		data.Capacity, partial = nativeComputeQuantitySet(object.Object, "status", "capacity")
		allocatable, allocatablePartial := nativeComputeQuantitySet(object.Object, "status", "allocatable")
		data.Allocatable = allocatable
		partial = partial || allocatablePartial
	case "Pod":
		data.Requests, partial = podComputeQuantitySet(object.Object, "requests")
		limits, limitsPartial := podComputeQuantitySet(object.Object, "limits")
		data.Limits = limits
		partial = partial || limitsPartial
		data.Capacity = notApplicableComputeQuantitySet("Pod resources do not provide node capacity fields.")
		data.Allocatable = notApplicableComputeQuantitySet("Pod resources do not provide node allocatable fields.")
	default:
		data.Requests = notApplicableComputeQuantitySet("Compute values are supported only for core/v1 Nodes and Pods.")
		data.Limits = notApplicableComputeQuantitySet("Compute values are supported only for core/v1 Nodes and Pods.")
		data.Capacity = notApplicableComputeQuantitySet("Compute values are supported only for core/v1 Nodes and Pods.")
		data.Allocatable = notApplicableComputeQuantitySet("Compute values are supported only for core/v1 Nodes and Pods.")
		partial = true
	}
	return data, partial
}

// notApplicableComputeQuantitySet marks a quantity family that Kubernetes does not define for this resource.
func notApplicableComputeQuantitySet(message string) ComputeQuantitySet {
	return ComputeQuantitySet{
		CPU:     ComputeQuantityValue{State: computeQuantityStateNotApplicable},
		Memory:  ComputeQuantityValue{State: computeQuantityStateNotApplicable},
		State:   computeQuantityStateNotApplicable,
		Message: message,
	}
}

// nativeComputeQuantitySet reads only CPU and memory from a native Node quantity map.
func nativeComputeQuantitySet(object map[string]interface{}, fields ...string) (ComputeQuantitySet, bool) {
	values := readPackMap(object, fields...)
	if values.Malformed {
		partialValue := ComputeQuantityValue{State: computeQuantityStatePartial}
		return ComputeQuantitySet{CPU: partialValue, Memory: partialValue, State: computeQuantityStatePartial, Message: "The native Node quantity map was malformed; safe CPU and memory values were withheld."}, true
	}
	if !values.Found {
		return ComputeQuantitySet{
			CPU:     ComputeQuantityValue{State: computeQuantityStateAbsent},
			Memory:  ComputeQuantityValue{State: computeQuantityStateAbsent},
			State:   computeQuantityStateAbsent,
			Message: "The Node did not report this quantity family.",
		}, false
	}
	cpu, cpuMalformed := computeQuantityValue(values.Value, "cpu")
	memory, memoryMalformed := computeQuantityValue(values.Value, "memory")
	result := newComputeQuantitySet(cpu, memory, "Native Kubernetes Node quantities are shown without provider-specific fields.")
	partial := cpuMalformed || memoryMalformed
	if partial {
		result.State = computeQuantityStatePartial
		result.Message = "Some native Node CPU or memory quantities were absent or malformed; the remaining safe values are shown."
	}
	return result, partial
}

// podComputeQuantitySet aggregates one Pod resource requirement with Kubernetes init-container semantics.
func podComputeQuantitySet(object map[string]interface{}, resourceField string) (ComputeQuantitySet, bool) {
	cpu, cpuPartial := aggregatePodComputeQuantity(object, resourceField, "cpu")
	memory, memoryPartial := aggregatePodComputeQuantity(object, resourceField, "memory")
	result := newComputeQuantitySet(cpu, memory, "Derived from container resource quantities using Kubernetes quantity arithmetic.")
	partial := cpuPartial || memoryPartial
	if partial {
		result.State = computeQuantityStatePartial
		result.Message = "Some container CPU or memory quantities were absent or malformed; shown totals are partial."
	}
	if result.State == computeQuantityStateAbsent {
		result.Message = "No container CPU or memory quantity was declared."
	}
	return result, partial
}

// aggregatePodComputeQuantity sums app containers and takes the maximum init-container quantity.
func aggregatePodComputeQuantity(object map[string]interface{}, resourceField, resourceName string) (ComputeQuantityValue, bool) {
	var applicationTotal resource.Quantity
	var initMaximum resource.Quantity
	hasApplication := false
	hasInit := false
	missing := false
	partial := false

	for _, source := range []struct {
		field  string
		isInit bool
	}{
		{field: "containers"},
		{field: "initContainers", isInit: true},
		{field: "ephemeralContainers"},
	} {
		containers := readPackSlice(object, "spec", source.field)
		partial = partial || containers.Malformed
		if !containers.Found {
			continue
		}
		for index, raw := range containers.Value {
			if index >= readPackContainerLimit {
				partial = true
				break
			}
			container, ok := raw.(map[string]interface{})
			if !ok {
				partial = true
				continue
			}
			quantity, found, malformed := podContainerComputeQuantity(container, resourceField, resourceName)
			if malformed {
				partial = true
				continue
			}
			if !found {
				missing = true
				continue
			}
			if source.isInit {
				if !hasInit || quantity.Cmp(initMaximum) > 0 {
					initMaximum = quantity.DeepCopy()
					hasInit = true
				}
				continue
			}
			if !hasApplication {
				applicationTotal = quantity.DeepCopy()
				hasApplication = true
			} else {
				applicationTotal.Add(quantity)
			}
		}
	}

	if !hasApplication && !hasInit {
		if partial {
			return ComputeQuantityValue{State: computeQuantityStatePartial}, true
		}
		return ComputeQuantityValue{State: computeQuantityStateAbsent}, false
	}
	effective := resource.Quantity{}
	if hasApplication {
		effective = applicationTotal.DeepCopy()
	}
	if hasInit && (!hasApplication || initMaximum.Cmp(effective) > 0) {
		effective = initMaximum.DeepCopy()
	}
	text, valid := boundedComputeQuantityString(effective)
	if !valid {
		return ComputeQuantityValue{State: computeQuantityStatePartial}, true
	}
	value := ComputeQuantityValue{Value: text, State: computeQuantityStateAvailable}
	if missing || partial {
		value.State = computeQuantityStatePartial
	}
	return value, missing || partial
}

// podContainerComputeQuantity parses one bounded container resource quantity without exposing its object data.
func podContainerComputeQuantity(container map[string]interface{}, resourceField, resourceName string) (resource.Quantity, bool, bool) {
	resources := readPackMap(container, "resources")
	if resources.Malformed {
		return resource.Quantity{}, false, true
	}
	if !resources.Found {
		return resource.Quantity{}, false, false
	}
	quantities := readPackMap(resources.Value, resourceField)
	if quantities.Malformed {
		return resource.Quantity{}, false, true
	}
	if !quantities.Found {
		return resource.Quantity{}, false, false
	}
	raw, found := quantities.Value[resourceName]
	if !found {
		return resource.Quantity{}, false, false
	}
	text, valid := readPackQuantityValue(raw)
	if !valid || strings.TrimSpace(text) == "" {
		return resource.Quantity{}, false, true
	}
	quantity, err := resource.ParseQuantity(text)
	if err != nil {
		return resource.Quantity{}, false, true
	}
	return quantity, true, false
}

// computeQuantityValue reads one bounded quantity from a native Kubernetes map.
func computeQuantityValue(values map[string]interface{}, key string) (ComputeQuantityValue, bool) {
	raw, found := values[key]
	if !found {
		return ComputeQuantityValue{State: computeQuantityStateAbsent}, false
	}
	text, valid := readPackQuantityValue(raw)
	if !valid || strings.TrimSpace(text) == "" {
		return ComputeQuantityValue{State: computeQuantityStatePartial}, true
	}
	quantity, err := resource.ParseQuantity(text)
	if err != nil {
		return ComputeQuantityValue{State: computeQuantityStatePartial}, true
	}
	normalized, bounded := boundedComputeQuantityString(quantity)
	if !bounded {
		return ComputeQuantityValue{State: computeQuantityStatePartial}, true
	}
	return ComputeQuantityValue{Value: normalized, State: computeQuantityStateAvailable}, false
}

// newComputeQuantitySet combines per-resource quantity states into one explicit family state.
func newComputeQuantitySet(cpu, memory ComputeQuantityValue, message string) ComputeQuantitySet {
	state := computeQuantityStateAvailable
	switch {
	case cpu.State == computeQuantityStateNotApplicable && memory.State == computeQuantityStateNotApplicable:
		state = computeQuantityStateNotApplicable
	case cpu.State == computeQuantityStateAbsent && memory.State == computeQuantityStateAbsent:
		state = computeQuantityStateAbsent
	case cpu.State != computeQuantityStateAvailable || memory.State != computeQuantityStateAvailable:
		state = computeQuantityStatePartial
	}
	return ComputeQuantitySet{CPU: cpu, Memory: memory, State: state, Message: message}
}

// alignComputeUsageUnits renders actual CPU and memory in the corresponding object-side unit.
func alignComputeUsageUnits(data *ComputeReadPackData) {
	if data == nil {
		return
	}
	data.Usage.CPU = alignComputeQuantityUnit(data.Usage.CPU, computeReferenceQuantity(data.Requests.CPU, data.Limits.CPU, data.Capacity.CPU, data.Allocatable.CPU))
	data.Usage.Memory = alignComputeQuantityUnit(data.Usage.Memory, computeReferenceQuantity(data.Requests.Memory, data.Limits.Memory, data.Capacity.Memory, data.Allocatable.Memory))
}

// computeReferenceQuantity selects the first displayed object-side quantity that provides a unit.
func computeReferenceQuantity(values ...ComputeQuantityValue) string {
	for _, value := range values {
		if strings.TrimSpace(value.Value) != "" {
			return value.Value
		}
	}
	return ""
}

// alignComputeQuantityUnit converts one actual quantity to the unit suffix used by a reference quantity.
func alignComputeQuantityUnit(value ComputeQuantityValue, reference string) ComputeQuantityValue {
	if strings.TrimSpace(value.Value) == "" || strings.TrimSpace(reference) == "" {
		return value
	}
	actual, actualErr := resource.ParseQuantity(value.Value)
	unitSuffix := computeQuantityUnitSuffix(reference)
	unit, unitErr := resource.ParseQuantity("1" + unitSuffix)
	if actualErr != nil || unitErr != nil {
		return value
	}
	actualDecimal := actual.AsDec()
	normalized := actualDecimal.QuoExact(actualDecimal, unit.AsDec()).String()
	if unitSuffix != "" {
		normalized += unitSuffix
	}
	if len(normalized) > readPackScalarLimit {
		return value
	}
	value.Value = normalized
	return value
}

// computeQuantityUnitSuffix extracts a Kubernetes display suffix from a normalized quantity.
func computeQuantityUnitSuffix(value string) string {
	for _, suffix := range []string{"Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "n", "u", "m", "k", "K", "M", "G", "T", "P", "E"} {
		if strings.HasSuffix(strings.TrimSpace(value), suffix) {
			return suffix
		}
	}
	return ""
}

// readComputeUsage retrieves one exact Node or namespaced Pod metrics object through the authenticated dynamic client.
func readComputeUsage(ctx context.Context, client dynamic.Interface, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget, now time.Time) ComputeUsageSummary {
	if object == nil || object.GetName() == "" {
		return failedComputeUsage(readPackStatePartial, "The resource identity was incomplete; actual usage was withheld.")
	}
	metricDefinition := ResourceDefinition{
		Group:      "metrics.k8s.io",
		Version:    "v1beta1",
		Resource:   definition.Resource,
		Namespaced: definition.Namespaced,
	}
	if definition.Namespaced && strings.TrimSpace(object.GetNamespace()) == "" {
		return failedComputeUsage(readPackStatePartial, "The Pod namespace was missing; exact metrics identity could not be verified.")
	}
	metric, err := getOperationalResource(ctx, client, budget, metricDefinition, object.GetNamespace(), object.GetName())
	if err != nil {
		return failedComputeUsage(operationalReadPackErrorState(err), operationalReadPackErrorMessage(err))
	}
	if !computeMetricIdentityMatches(definition, object, metric) {
		return failedComputeUsage(readPackStatePartial, "The metrics response identity did not match the requested resource; actual usage was withheld.")
	}
	return parseComputeMetricUsage(metric.Object, now)
}

// computeMetricIdentityMatches rejects same-named metrics from a different namespace or resource identity.
func computeMetricIdentityMatches(definition ResourceDefinition, object, metric *unstructured.Unstructured) bool {
	if object == nil || metric == nil || object.GetName() == "" || metric.GetName() != object.GetName() {
		return false
	}
	if definition.Namespaced {
		return metric.GetNamespace() == object.GetNamespace()
	}
	return metric.GetNamespace() == ""
}

// parseComputeMetricUsage normalizes one metrics.k8s.io object while preserving its sample timestamp.
func parseComputeMetricUsage(object map[string]interface{}, now time.Time) ComputeUsageSummary {
	timestamp, timestampErr := parseMetricTimestamp(object, "compute metrics")
	var timestampPointer *time.Time
	if timestampErr == nil {
		timestampPointer = &timestamp
	}

	usage, found, usageErr := unstructured.NestedMap(object, "usage")
	if timestampErr != nil {
		return ComputeUsageSummary{
			CPU:       ComputeQuantityValue{State: readPackStateError},
			Memory:    ComputeQuantityValue{State: readPackStateError},
			State:     readPackStateError,
			Message:   "The metrics sample timestamp was missing or malformed; actual usage freshness could not be established.",
			Timestamp: timestampPointer,
		}
	}
	if usageErr != nil {
		result := failedComputeUsage(readPackStatePartial, "The metrics usage map was malformed; actual usage was withheld.")
		result.Timestamp = timestampPointer
		return result
	}
	if !found {
		return parseComputePodMetricUsage(object, timestampPointer, now)
	}

	cpu, cpuMalformed := computeQuantityValue(usage, "cpu")
	memory, memoryMalformed := computeQuantityValue(usage, "memory")
	if cpuMalformed {
		cpu.State = readPackStatePartial
	}
	if memoryMalformed {
		memory.State = readPackStatePartial
	}
	result := ComputeUsageSummary{CPU: cpu, Memory: memory, Timestamp: timestampPointer}
	switch {
	case cpuMalformed || memoryMalformed:
		result.State = readPackStatePartial
		result.Message = "metrics.k8s.io returned a malformed CPU or memory quantity; remaining safe usage is shown."
	case cpu.State == computeQuantityStateAbsent && memory.State == computeQuantityStateAbsent:
		result.CPU.State = readPackStateEmpty
		result.Memory.State = readPackStateEmpty
		result.State = readPackStateEmpty
		result.Message = "metrics.k8s.io returned no CPU or memory usage values for this object."
	case cpu.State == computeQuantityStateAbsent || memory.State == computeQuantityStateAbsent:
		result.State = readPackStatePartial
		result.Message = "metrics.k8s.io returned only part of the CPU and memory usage sample."
	case now.After(timestamp.Add(metricsStaleAfter)):
		result.State = metricsStateStale
		result.Message = "The metrics sample is older than the native freshness window; actual usage may be stale."
	default:
		result.State = readPackStateAvailable
		result.Message = "Actual usage is read from the authenticated metrics.k8s.io API."
	}
	return result
}

// parseComputePodMetricUsage aggregates bounded per-container PodMetrics usage with exact quantity arithmetic.
func parseComputePodMetricUsage(object map[string]interface{}, timestamp *time.Time, now time.Time) ComputeUsageSummary {
	containers, found, err := unstructured.NestedSlice(object, "containers")
	if err != nil {
		result := failedComputeUsage(readPackStatePartial, "The Pod metrics container list was malformed; actual usage was withheld.")
		result.Timestamp = timestamp
		return result
	}
	if !found || len(containers) == 0 {
		return ComputeUsageSummary{
			CPU:       ComputeQuantityValue{State: readPackStateEmpty},
			Memory:    ComputeQuantityValue{State: readPackStateEmpty},
			State:     readPackStateEmpty,
			Message:   "metrics.k8s.io returned no Pod container usage values for this object.",
			Timestamp: timestamp,
		}
	}

	var cpuTotal resource.Quantity
	var memoryTotal resource.Quantity
	hasCPU := false
	hasMemory := false
	cpuMissing := false
	memoryMissing := false
	partial := false
	for index, raw := range containers {
		if index >= readPackContainerLimit {
			partial = true
			break
		}
		container, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		usage, usageFound, usageErr := unstructured.NestedMap(container, "usage")
		if usageErr != nil || !usageFound {
			partial = true
			continue
		}
		cpu, cpuMalformed := computeQuantityValue(usage, "cpu")
		memory, memoryMalformed := computeQuantityValue(usage, "memory")
		partial = partial || cpuMalformed || memoryMalformed
		if cpu.State == computeQuantityStateAbsent {
			cpuMissing = true
		} else if cpu.State == computeQuantityStateAvailable {
			quantity, parseErr := resource.ParseQuantity(cpu.Value)
			if parseErr != nil {
				partial = true
			} else if !hasCPU {
				cpuTotal = quantity.DeepCopy()
				hasCPU = true
			} else {
				cpuTotal.Add(quantity)
			}
		}
		if memory.State == computeQuantityStateAbsent {
			memoryMissing = true
		} else if memory.State == computeQuantityStateAvailable {
			quantity, parseErr := resource.ParseQuantity(memory.Value)
			if parseErr != nil {
				partial = true
			} else if !hasMemory {
				memoryTotal = quantity.DeepCopy()
				hasMemory = true
			} else {
				memoryTotal.Add(quantity)
			}
		}
	}

	cpu := aggregateComputeMetricValue(cpuTotal, hasCPU, cpuMissing, partial)
	memory := aggregateComputeMetricValue(memoryTotal, hasMemory, memoryMissing, partial)
	result := ComputeUsageSummary{CPU: cpu, Memory: memory, Timestamp: timestamp}
	switch {
	case partial:
		result.State = readPackStatePartial
		result.Message = "metrics.k8s.io returned an incomplete Pod container usage sample; remaining safe usage is shown."
	case !hasCPU && !hasMemory:
		result.CPU.State = readPackStateEmpty
		result.Memory.State = readPackStateEmpty
		result.State = readPackStateEmpty
		result.Message = "metrics.k8s.io returned no CPU or memory usage values for this Pod."
	case cpuMissing || memoryMissing:
		result.State = readPackStatePartial
		result.Message = "metrics.k8s.io returned only part of the Pod CPU and memory usage sample."
	case timestamp == nil:
		result.State = readPackStateError
		result.Message = "The Pod metrics sample timestamp was missing; actual usage freshness could not be established."
	case now.After(timestamp.Add(metricsStaleAfter)):
		result.State = metricsStateStale
		result.Message = "The Pod metrics sample is older than the native freshness window; actual usage may be stale."
	default:
		result.State = readPackStateAvailable
		result.Message = "Actual Pod usage is read from the authenticated metrics.k8s.io API."
	}
	return result
}

// aggregateComputeMetricValue converts an exact aggregate into a bounded usage value and completeness state.
func aggregateComputeMetricValue(total resource.Quantity, found, missing, partial bool) ComputeQuantityValue {
	if !found {
		if partial {
			return ComputeQuantityValue{State: readPackStatePartial}
		}
		return ComputeQuantityValue{State: computeQuantityStateAbsent}
	}
	text, valid := boundedComputeQuantityString(total)
	if !valid {
		return ComputeQuantityValue{State: readPackStatePartial}
	}
	value := ComputeQuantityValue{Value: text, State: computeQuantityStateAvailable}
	if missing || partial {
		value.State = computeQuantityStatePartial
	}
	return value
}

// boundedComputeQuantityString limits normalized aggregate output to the existing read-pack scalar bound.
func boundedComputeQuantityString(quantity resource.Quantity) (string, bool) {
	text := quantity.String()
	return text, len(text) <= readPackScalarLimit
}

// failedComputeUsage creates a safe usage state without returning Kubernetes error text or raw metrics data.
func failedComputeUsage(state, message string) ComputeUsageSummary {
	return ComputeUsageSummary{
		CPU:     ComputeQuantityValue{State: state},
		Memory:  ComputeQuantityValue{State: state},
		State:   state,
		Message: message,
	}
}

// computeReadPackState derives a panel state without hiding object-side values behind telemetry failures.
func computeReadPackState(data ComputeReadPackData, objectPartial bool) string {
	if objectPartial || data.Requests.State == computeQuantityStatePartial || data.Limits.State == computeQuantityStatePartial || data.Capacity.State == computeQuantityStatePartial || data.Allocatable.State == computeQuantityStatePartial {
		return readPackStatePartial
	}
	switch data.Usage.State {
	case readPackStateAvailable, readPackStateStale, readPackStatePermissionDenied, readPackStateUnavailable, readPackStateEmpty, readPackStatePartial, readPackStateError:
		return data.Usage.State
	default:
		return readPackStateError
	}
}

// computeReadPackMessage explains telemetry state while affirming that object-side fields remain visible.
func computeReadPackMessage(data ComputeReadPackData, objectPartial bool) string {
	if objectPartial {
		return "Some object-side compute values are incomplete; safe values remain visible. " + data.Usage.Message
	}
	if data.Usage.State == readPackStateAvailable {
		return "Object-side compute values and the current metrics.k8s.io usage sample are available."
	}
	if data.Usage.Message != "" {
		return "Object-side compute values remain visible. " + data.Usage.Message
	}
	return "Object-side compute values remain visible; actual usage was not reported."
}
