package kube

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestCustomResourceCapabilitiesForCRDNormalizesAllServedVersions verifies preferred, storage, scope, subresource, and column metadata.
func TestCustomResourceCapabilitiesForCRDNormalizesAllServedVersions(t *testing.T) {
	crd := testVersionedCRD()
	capabilities, ok := customResourceCapabilitiesForCRD(crd, map[string]string{"widgets.example.com": "v1beta1"})
	if !ok || len(capabilities) != 2 {
		t.Fatalf("versioned CRD metadata was not normalized: ok=%t capabilities=%+v", ok, capabilities)
	}
	for _, version := range []string{"v1", "v1beta1"} {
		capability := capabilities[version]
		if capability == nil || capability.Group != "widgets.example.com" || capability.Kind != "Widget" || capability.Plural != "widgets" || capability.Resource != "widgets" || capability.Scope != "Namespaced" {
			t.Fatalf("CRD identity was incomplete for %s: %+v", version, capability)
		}
		if len(capability.ServedVersions) != 2 || capability.PreferredVersion != "v1beta1" || capability.StorageVersion != "v1" || !capability.StatusSubresource || !capability.ScaleSubresource {
			t.Fatalf("CRD version metadata was incomplete for %s: %+v", version, capability)
		}
		if capability.ScaleSpecReplicasPath != ".spec.replicas" || capability.ScaleStatusReplicasPath != ".status.replicas" || capability.ScaleLabelSelectorPath != ".status.selector" {
			t.Fatalf("scale metadata was not safely normalized for %s: %+v", version, capability)
		}
	}
	if len(capabilities["v1"].PrinterColumns) != 1 || capabilities["v1"].PrinterColumns[0].Name != "Phase" || capabilities["v1"].PrinterColumns[0].Path[0] != "status" || capabilities["v1"].PrinterColumns[0].Path[1] != "phase" {
		t.Fatalf("safe printer column metadata was not preserved: %+v", capabilities["v1"].PrinterColumns)
	}
}

// TestCustomResourceCapabilitiesRejectsUnsupportedPrinterColumns verifies malformed JSONPath columns fall back without affecting valid columns.
func TestCustomResourceCapabilitiesRejectsUnsupportedPrinterColumns(t *testing.T) {
	crd := testVersionedCRD()
	spec := crd.Object["spec"].(map[string]interface{})
	versions := spec["versions"].([]interface{})
	version := versions[0].(map[string]interface{})
	version["additionalPrinterColumns"] = []interface{}{
		map[string]interface{}{"name": "Phase", "type": "string", "jsonPath": ".status.phase"},
		map[string]interface{}{"name": "Unsafe", "type": "string", "jsonPath": ".status.conditions[?(@.type==\"Ready\")].status"},
		map[string]interface{}{"name": "Object", "type": "object", "jsonPath": ".status"},
	}
	capabilities, ok := customResourceCapabilitiesForCRD(crd, nil)
	if !ok || len(capabilities["v1"].PrinterColumns) != 1 || len(capabilities["v1"].PrinterColumnWarnings) != 2 {
		t.Fatalf("unsupported printer columns did not use safe fallback: %+v", capabilities["v1"])
	}
	item := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{"phase": "Ready", "conditions": []interface{}{map[string]interface{}{"status": "True"}}},
	}}
	values := evaluateCustomResourcePrinterColumns(item, capabilities["v1"])
	if values["Phase"] != "Ready" || values["Unsafe"] != "" {
		t.Fatalf("printer columns were evaluated unsafely: %+v", values)
	}
}

// TestParseCRDResourceSchemaReportsFieldFallbacksAndStatusReadOnly verifies partial forms retain YAML for unsupported fields.
func TestParseCRDResourceSchemaReportsFieldFallbacksAndStatusReadOnly(t *testing.T) {
	crd := testVersionedCRD()
	spec := crd.Object["spec"].(map[string]interface{})
	versions := spec["versions"].([]interface{})
	version := versions[0].(map[string]interface{})
	version["schema"] = map[string]interface{}{"openAPIV3Schema": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
					"mode": map[string]interface{}{"type": "string", "oneOf": []interface{}{map[string]interface{}{"type": "string"}}},
				},
			},
			"status": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"phase": map[string]interface{}{"type": "string"}},
			},
		},
	}}
	parsed := parseCRDResourceSchemaDetails(crd, "widgets.example.com", "v1", "widgets", true)
	if parsed.state != resourceSchemaStateAvailable || parsed.field == nil || len(parsed.fallbacks) != 1 || !parsed.statusSubresource {
		t.Fatalf("partial schema result was incomplete: %+v", parsed)
	}
	if parsed.fallbacks[0].Path != "$.spec.mode" || parsed.fallbacks[0].Reason == "" {
		t.Fatalf("field fallback explanation was not bounded: %+v", parsed.fallbacks)
	}
	var statusField *SchemaField
	for index := range parsed.field.Properties {
		if parsed.field.Properties[index].Name == "status" {
			statusField = &parsed.field.Properties[index]
		}
	}
	if statusField == nil || !statusField.ReadOnly || len(statusField.Properties) != 1 || !statusField.Properties[0].ReadOnly {
		t.Fatalf("status subresource fields were not read-only: %+v", parsed.field.Properties)
	}
}

// TestParseCRDResourceSchemaMarksScaleStatusPathReadOnly verifies scale status fields stay non-writable without a full status subresource.
func TestParseCRDResourceSchemaMarksScaleStatusPathReadOnly(t *testing.T) {
	crd := testVersionedCRD()
	spec := crd.Object["spec"].(map[string]interface{})
	subresources := spec["subresources"].(map[string]interface{})
	delete(subresources, "status")
	versions := spec["versions"].([]interface{})
	versions[0].(map[string]interface{})["schema"] = map[string]interface{}{"openAPIV3Schema": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"replicas": map[string]interface{}{"type": "integer"}, "phase": map[string]interface{}{"type": "string"}},
			},
		},
	}}
	parsed := parseCRDResourceSchemaDetails(crd, "widgets.example.com", "v1", "widgets", true)
	if parsed.state != resourceSchemaStateAvailable || parsed.statusSubresource || parsed.scaleStatusPath != ".status.replicas" {
		t.Fatalf("scale status metadata was not preserved: %+v", parsed)
	}
	status := parsed.field.Properties[0]
	var replicasReadOnly, phaseReadOnly bool
	for _, property := range status.Properties {
		if property.Name == "replicas" {
			replicasReadOnly = property.ReadOnly
		}
		if property.Name == "phase" {
			phaseReadOnly = property.ReadOnly
		}
	}
	if status.Name != "status" || !replicasReadOnly || phaseReadOnly {
		t.Fatalf("scale status field was not read-only without locking unrelated status fields: %+v", status)
	}
}

// TestParseCRDResourceSchemaBoundsPartialTraversalDepth verifies deep schemas become bounded field fallbacks instead of deep recursion.
func TestParseCRDResourceSchemaBoundsPartialTraversalDepth(t *testing.T) {
	crd := testVersionedCRD()
	spec := crd.Object["spec"].(map[string]interface{})
	versions := spec["versions"].([]interface{})
	version := versions[0].(map[string]interface{})
	root := map[string]interface{}{"type": "object"}
	current := root
	for depth := 0; depth <= resourceSchemaMaxDepth+2; depth++ {
		child := map[string]interface{}{"type": "object"}
		current["properties"] = map[string]interface{}{fmt.Sprintf("level-%d", depth): child}
		current = child
	}
	current["properties"] = map[string]interface{}{"value": map[string]interface{}{"type": "string"}}
	version["schema"] = map[string]interface{}{"openAPIV3Schema": root}

	parsed := parseCRDResourceSchemaDetails(crd, "widgets.example.com", "v1", "widgets", true)
	if parsed.state != resourceSchemaStateAvailable || parsed.field == nil || len(parsed.fallbacks) == 0 || !strings.Contains(parsed.fallbacks[0].Reason, "nesting exceeds") {
		t.Fatalf("deep schema did not use a bounded fallback: state=%q field=%v fallbacks=%+v", parsed.state, parsed.field != nil, parsed.fallbacks)
	}
}

// TestParseCRDResourceSchemaRejectsOversizedRootDuringPartialTraversal verifies root field overflow returns a schema-level YAML fallback.
func TestParseCRDResourceSchemaRejectsOversizedRootDuringPartialTraversal(t *testing.T) {
	crd := testVersionedCRD()
	spec := crd.Object["spec"].(map[string]interface{})
	versions := spec["versions"].([]interface{})
	version := versions[0].(map[string]interface{})
	properties := make(map[string]interface{}, resourceSchemaMaxFields)
	for index := 0; index < resourceSchemaMaxFields; index++ {
		properties[fmt.Sprintf("field-%03d", index)] = map[string]interface{}{"type": "string"}
	}
	version["schema"] = map[string]interface{}{"openAPIV3Schema": map[string]interface{}{"type": "object", "properties": properties}}

	parsed := parseCRDResourceSchemaDetails(crd, "widgets.example.com", "v1", "widgets", true)
	if parsed.state != resourceSchemaStateUnsupported || parsed.field != nil || !strings.Contains(parsed.message, "more than") {
		t.Fatalf("oversized root schema did not use a safe schema-level fallback: %+v", parsed)
	}
}

// TestParseCRDResourceSchemaFallsBackForOversizedNestedField verifies nested field overflow preserves the supported root form.
func TestParseCRDResourceSchemaFallsBackForOversizedNestedField(t *testing.T) {
	crd := testVersionedCRD()
	spec := crd.Object["spec"].(map[string]interface{})
	versions := spec["versions"].([]interface{})
	version := versions[0].(map[string]interface{})
	nestedProperties := make(map[string]interface{}, resourceSchemaMaxFields)
	for index := 0; index < resourceSchemaMaxFields; index++ {
		nestedProperties[fmt.Sprintf("field-%03d", index)] = map[string]interface{}{"type": "string"}
	}
	version["schema"] = map[string]interface{}{"openAPIV3Schema": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{"type": "object", "properties": nestedProperties},
		},
	}}

	parsed := parseCRDResourceSchemaDetails(crd, "widgets.example.com", "v1", "widgets", true)
	if parsed.state != resourceSchemaStateAvailable || parsed.field == nil || len(parsed.fallbacks) != 1 || parsed.fallbacks[0].Path != "$.spec" || parsed.fallbacksTruncated {
		t.Fatalf("oversized nested schema did not preserve a bounded field fallback: %+v", parsed)
	}
}

// TestSummarizeResourceEvaluatesOnlyConfiguredPrinterColumns verifies list summaries retain resource-version metadata and safe column values.
func TestSummarizeResourceEvaluatesOnlyConfiguredPrinterColumns(t *testing.T) {
	item := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "blue", "resourceVersion": "19"},
		"status":   map[string]interface{}{"phase": "Ready"},
	}}
	definition := ResourceDefinition{CustomResourceCapabilities: &CustomResourceCapabilities{
		PrinterColumns: []CustomResourcePrinterColumn{{Name: "Phase", Type: "string", Path: []string{"status", "phase"}}},
	}}
	summary := summarizeResource(item, definition)
	if summary.Name != "blue" || summary.ResourceVersion != "19" || summary.PrinterColumns["Phase"] != "Ready" {
		t.Fatalf("custom resource summary lost identity or printer value: %+v", summary)
	}
}

// testVersionedCRD constructs a bounded multi-version CRD fixture for capability tests.
func testVersionedCRD() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]interface{}{"name": "widgets.widgets.example.com"},
		"spec": map[string]interface{}{
			"group": "widgets.example.com",
			"names": map[string]interface{}{"plural": "widgets", "kind": "Widget"},
			"scope": "Namespaced",
			"subresources": map[string]interface{}{
				"status": map[string]interface{}{},
				"scale": map[string]interface{}{
					"specReplicasPath":   ".spec.replicas",
					"statusReplicasPath": ".status.replicas",
					"labelSelectorPath":  ".status.selector",
				},
			},
			"versions": []interface{}{
				map[string]interface{}{
					"name":                     "v1",
					"served":                   true,
					"storage":                  true,
					"schema":                   map[string]interface{}{"openAPIV3Schema": map[string]interface{}{"type": "object"}},
					"additionalPrinterColumns": []interface{}{map[string]interface{}{"name": "Phase", "type": "string", "jsonPath": ".status.phase"}},
				},
				map[string]interface{}{
					"name":    "v1beta1",
					"served":  true,
					"storage": false,
					"schema":  map[string]interface{}{"openAPIV3Schema": map[string]interface{}{"type": "object"}},
				},
				map[string]interface{}{"name": "v1alpha1", "served": false, "storage": false},
			},
		},
	}}
}
