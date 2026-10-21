package kube

import (
	"context"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestParseCRDResourceSchemaSupportsStructuralFields verifies nested controls and validation metadata survive normalization.
func TestParseCRDResourceSchemaSupportsStructuralFields(t *testing.T) {
	openAPISchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{
				"type":        "object",
				"description": "Desired widget state.",
				"required":    []interface{}{"mode", "settings"},
				"properties": map[string]interface{}{
					"mode": map[string]interface{}{
						"type":    "string",
						"enum":    []interface{}{"active", "paused"},
						"default": "active",
					},
					"settings": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
					},
					"ports": map[string]interface{}{
						"type":     "array",
						"minItems": int64(1),
						"items": map[string]interface{}{
							"type":    "integer",
							"minimum": float64(1),
						},
					},
				},
			},
		},
	}
	field, state, message := parseCRDResourceSchema(testCRD(openAPISchema), "widgets.example.com", "v1", "widgets")
	if state != resourceSchemaStateAvailable || message != "" || field == nil {
		t.Fatalf("unexpected schema result: state=%q message=%q field=%+v", state, message, field)
	}
	if len(field.Properties) != 1 || field.Properties[0].Name != "spec" || field.Properties[0].Description != "Desired widget state." {
		t.Fatalf("root properties were not normalized: %+v", field.Properties)
	}
	spec := field.Properties[0]
	if len(spec.Properties) != 3 || !spec.Properties[0].Required || !spec.Properties[2].Required {
		t.Fatalf("required nested properties were not preserved: %+v", spec.Properties)
	}
	mode := spec.Properties[0]
	if mode.Name != "mode" || len(mode.Enum) != 2 || mode.Default != "active" {
		t.Fatalf("enum/default metadata was not preserved: %+v", mode)
	}
	ports := spec.Properties[1]
	if ports.Name != "ports" || ports.Items == nil || ports.Items.Type != "integer" || ports.MinItems == nil || *ports.MinItems != 1 || ports.Items.Minimum == nil || *ports.Items.Minimum != 1 {
		t.Fatalf("array validation metadata was not preserved: %+v", ports)
	}
	settings := spec.Properties[2]
	if settings.Name != "settings" || settings.AdditionalProperties == nil || settings.AdditionalProperties.Type != "string" {
		t.Fatalf("typed map schema was not preserved: %+v", settings)
	}
}

// TestLoadResourceSchemaUsesDynamicCRDAPI verifies schema retrieval stays on the generic Kubernetes API.
func TestLoadResourceSchemaUsesDynamicCRDAPI(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), testCRD(map[string]interface{}{
		"type": "object",
	}))
	result, err := loadResourceSchema(context.Background(), client, "widgets.example.com", "v1", "widgets")
	if err != nil || result.State != resourceSchemaStateAvailable || result.Kind != "Widget" || result.Schema == nil {
		t.Fatalf("dynamic CRD schema retrieval failed: result=%+v err=%v", result, err)
	}
}

// TestLoadResourceSchemaClassifiesRBACDenial verifies schema access failures are explicit and value-free.
func TestLoadResourceSchemaClassifiesRBACDenial(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("get", "customresourcedefinitions", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"}, "widgets.widgets.example.com", nil)
	})
	result, err := loadResourceSchema(context.Background(), client, "widgets.example.com", "v1", "widgets")
	if err != nil || result.State != resourceSchemaStatePermissionDenied || result.Schema != nil || strings.Contains(result.Message, "widgets.widgets.example.com") {
		t.Fatalf("RBAC denial was not safely classified: result=%+v err=%v", result, err)
	}
}

// TestParseCRDResourceSchemaUsesYAMLFallbackForUnsupportedConstructs verifies unsafe unions do not become guessed forms.
func TestParseCRDResourceSchemaUsesYAMLFallbackForUnsupportedConstructs(t *testing.T) {
	field, state, message := parseCRDResourceSchema(testCRD(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"value": map[string]interface{}{
				"type":  "string",
				"oneOf": []interface{}{map[string]interface{}{"type": "string"}},
			},
		},
	}), "widgets.example.com", "v1", "widgets")
	if field != nil || state != resourceSchemaStateUnsupported || !strings.Contains(message, "oneOf") {
		t.Fatalf("unsupported schema did not select YAML fallback: field=%+v state=%q message=%q", field, state, message)
	}
}

// TestParseCRDResourceSchemaClassifiesMissingSchema verifies old or non-structural CRDs remain generic YAML resources.
func TestParseCRDResourceSchemaClassifiesMissingSchema(t *testing.T) {
	crd := testCRD(nil)
	delete(crd.Object["spec"].(map[string]interface{})["versions"].([]interface{})[0].(map[string]interface{}), "schema")
	field, state, message := parseCRDResourceSchema(crd, "widgets.example.com", "v1", "widgets")
	if field != nil || state != resourceSchemaStateNonStructural || !strings.Contains(message, "structural") {
		t.Fatalf("missing structural schema was not classified: field=%+v state=%q message=%q", field, state, message)
	}
}

// TestMarkCustomResourceDefinitionsAnnotatesDiscoveredDefinitions verifies CRD discovery metadata is derived through a dynamic list.
func TestMarkCustomResourceDefinitionsAnnotatesDiscoveredDefinitions(t *testing.T) {
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		customResourceDefinitionGVR(): "CustomResourceDefinitionList",
	}, testCRD(map[string]interface{}{"type": "object"}))
	definitions := []ResourceDefinition{
		{Group: "widgets.example.com", Version: "v1", Resource: "widgets"},
		{Group: "widgets.example.com", Version: "v1beta1", Resource: "widgets"},
	}
	markCustomResourceDefinitions(context.Background(), client, definitions)
	if !definitions[0].CustomResource || definitions[1].CustomResource {
		t.Fatalf("custom resource annotations were incorrect: %+v", definitions)
	}
}

// testCRD constructs an unstructured v1 CustomResourceDefinition with an optional structural schema.
func testCRD(openAPISchema map[string]interface{}) *unstructured.Unstructured {
	version := map[string]interface{}{
		"name":    "v1",
		"served":  true,
		"storage": true,
	}
	if openAPISchema != nil {
		version["schema"] = map[string]interface{}{"openAPIV3Schema": openAPISchema}
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]interface{}{"name": "widgets.widgets.example.com"},
		"spec": map[string]interface{}{
			"group": "widgets.example.com",
			"names": map[string]interface{}{
				"plural": "widgets",
				"kind":   "Widget",
			},
			"scope":    "Namespaced",
			"versions": []interface{}{version},
		},
	}}
}
