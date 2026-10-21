package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/openapi"
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

// TestLoadResourceSchemaPrefersOpenAPIV3AndLocksServerFields verifies exact v3 retrieval and safe form metadata.
func TestLoadResourceSchemaPrefersOpenAPIV3AndLocksServerFields(t *testing.T) {
	discovery := testSchemaDiscovery(t, "Deployment", map[string]interface{}{
		"io.k8s.api.apps.v1.Deployment": map[string]interface{}{
			"type":        "object",
			"title":       "Workload deployment",
			"description": "A workload controller.",
			"properties": map[string]interface{}{
				"metadata": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":            map[string]interface{}{"type": "string"},
						"namespace":       map[string]interface{}{"type": "string"},
						"uid":             map[string]interface{}{"type": "string"},
						"resourceVersion": map[string]interface{}{"type": "string"},
						"labels":          map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
					},
				},
				"spec": map[string]interface{}{
					"type":        "object",
					"description": "Desired deployment state.",
					"required":    []interface{}{"mode"},
					"properties": map[string]interface{}{
						"mode": map[string]interface{}{
							"type":        "string",
							"format":      "hostname",
							"description": "Rollout mode.",
							"default":     "active",
							"enum":        []interface{}{"active", "paused"},
							"minLength":   float64(1),
						},
					},
				},
				"status": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ready": map[string]interface{}{"type": "boolean"},
					},
				},
			},
		},
	}, nil)
	result, err := loadResourceSchema(context.Background(), fake.NewSimpleDynamicClient(runtime.NewScheme()), "apps", "v1", "deployments", discovery)
	if err != nil || result.State != resourceSchemaStateAvailable || result.Source != resourceSchemaSourceOpenAPIV3 || result.Kind != "Deployment" || result.Schema == nil {
		t.Fatalf("OpenAPI v3 schema retrieval failed: result=%+v err=%v", result, err)
	}
	if result.Schema.Title != "Workload deployment" || result.Schema.Description != "A workload controller." {
		t.Fatalf("root descriptions were not preserved: %+v", result.Schema)
	}
	metadata := schemaFieldProperty(result.Schema, "metadata")
	nameField := schemaFieldProperty(metadata, "name")
	namespaceField := schemaFieldProperty(metadata, "namespace")
	uidField := schemaFieldProperty(metadata, "uid")
	resourceVersionField := schemaFieldProperty(metadata, "resourceVersion")
	if metadata == nil || nameField == nil || !nameField.ReadOnly || namespaceField == nil || !namespaceField.ReadOnly || uidField == nil || !uidField.ReadOnly || resourceVersionField == nil || !resourceVersionField.ReadOnly {
		t.Fatalf("server-owned metadata fields were not locked: %+v", metadata)
	}
	spec := schemaFieldProperty(result.Schema, "spec")
	mode := schemaFieldProperty(spec, "mode")
	if spec == nil || mode == nil || !mode.Required || mode.Description != "Rollout mode." || mode.Format != "hostname" || mode.Default != "active" || len(mode.Enum) != 2 || mode.MinLength == nil || *mode.MinLength != 1 {
		t.Fatalf("v3 field metadata was not normalized: spec=%+v mode=%+v", spec, mode)
	}
	status := schemaFieldProperty(result.Schema, "status")
	readyField := schemaFieldProperty(status, "ready")
	if status == nil || !status.ReadOnly || readyField == nil || !readyField.ReadOnly {
		t.Fatalf("status was writable in the normalized form: %+v", status)
	}
}

// TestLoadResourceSchemaResolvesBuiltInAllOfReferences verifies production-shaped bare definitions preserve every root section and safety lock.
func TestLoadResourceSchemaResolvesBuiltInAllOfReferences(t *testing.T) {
	discovery := testSchemaDiscovery(t, "Deployment", map[string]interface{}{
		"io.k8s.api.apps.v1.Deployment": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"apiVersion": map[string]interface{}{"type": "string"},
				"kind":       map[string]interface{}{"type": "string"},
				"metadata": map[string]interface{}{
					"allOf":       []interface{}{map[string]interface{}{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"}},
					"description": "Object metadata.",
				},
				"spec": map[string]interface{}{
					"allOf":       []interface{}{map[string]interface{}{"$ref": "#/components/schemas/io.k8s.api.apps.v1.DeploymentSpec"}},
					"description": "Desired deployment state.",
				},
				"status": map[string]interface{}{
					"allOf":    []interface{}{map[string]interface{}{"$ref": "#/components/schemas/io.k8s.api.apps.v1.DeploymentStatus"}},
					"readOnly": true,
				},
			},
		},
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":            map[string]interface{}{"type": "string"},
				"namespace":       map[string]interface{}{"type": "string"},
				"generateName":    map[string]interface{}{"type": "string"},
				"uid":             map[string]interface{}{"type": "string"},
				"labels":          map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
				"annotations":     map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
				"resourceVersion": map[string]interface{}{"type": "string"},
			},
		},
		"io.k8s.api.apps.v1.DeploymentSpec": map[string]interface{}{
			"type":     "object",
			"required": []interface{}{"replicas"},
			"properties": map[string]interface{}{
				"replicas": map[string]interface{}{"type": "integer", "minimum": float64(0)},
				"template": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		"io.k8s.api.apps.v1.DeploymentStatus": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"readyReplicas": map[string]interface{}{"type": "integer"},
				"conditions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"type":    map[string]interface{}{"type": "string"},
							"message": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
	}, nil)
	result, err := loadResourceSchema(context.Background(), fake.NewSimpleDynamicClient(runtime.NewScheme()), "apps", "v1", "deployments", discovery)
	if err != nil || result.State != resourceSchemaStateAvailable || result.Schema == nil {
		t.Fatalf("built-in allOf schema was not normalized: result=%+v err=%v", result, err)
	}
	for _, name := range []string{"apiVersion", "kind", "metadata", "spec", "status"} {
		if schemaFieldProperty(result.Schema, name) == nil {
			t.Fatalf("normalized root omitted %q: %+v", name, result.Schema.Properties)
		}
	}
	metadata := schemaFieldProperty(result.Schema, "metadata")
	if metadata.Description != "Object metadata." || schemaFieldProperty(metadata, "name") == nil || !schemaFieldProperty(metadata, "name").ReadOnly || schemaFieldProperty(metadata, "namespace") == nil || !schemaFieldProperty(metadata, "namespace").ReadOnly || schemaFieldProperty(metadata, "labels") == nil || schemaFieldProperty(metadata, "annotations") == nil || schemaFieldProperty(metadata, "generateName") == nil {
		t.Fatalf("metadata reference wrapper was not preserved safely: %+v", metadata)
	}
	spec := schemaFieldProperty(result.Schema, "spec")
	if spec.Description != "Desired deployment state." || schemaFieldProperty(spec, "replicas") == nil || schemaFieldProperty(spec, "template") == nil {
		t.Fatalf("nested spec fields did not survive normalization: %+v", spec)
	}
	status := schemaFieldProperty(result.Schema, "status")
	conditions := schemaFieldProperty(status, "conditions")
	var conditionType *SchemaField
	if conditions != nil && conditions.Items != nil {
		conditionType = schemaFieldProperty(conditions.Items, "type")
	}
	if !status.ReadOnly || conditions == nil || !conditions.ReadOnly || conditions.Items == nil || !conditions.Items.ReadOnly || conditionType == nil || !conditionType.ReadOnly {
		t.Fatalf("status descendants were not read-only: status=%+v conditions=%+v", status, conditions)
	}
}

// TestNormalizeOpenAPISchemaSupportsKubernetesScalarUnions verifies exact IntOrString and Quantity fields avoid YAML fallbacks without guessing real unions.
func TestNormalizeOpenAPISchemaSupportsKubernetesScalarUnions(t *testing.T) {
	intOrString := []interface{}{map[string]interface{}{"type": "integer"}, map[string]interface{}{"type": "string"}}
	quantity := []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "number"}}
	container := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"lifecycle": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"httpGet": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"port": map[string]interface{}{"oneOf": intOrString, "format": "int-or-string"}},
					},
				},
			},
			"readinessProbe": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"httpGet": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"port": map[string]interface{}{"oneOf": intOrString, "format": "int-or-string"}},
					},
				},
			},
			"env": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"valueFrom": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"resourceFieldRef": map[string]interface{}{
									"type":       "object",
									"properties": map[string]interface{}{"divisor": map[string]interface{}{"oneOf": quantity, "format": "quantity"}},
								},
							},
						},
					},
				},
			},
		},
	}
	normalized, err := normalizeOpenAPISchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{"allOf": []interface{}{map[string]interface{}{"$ref": "#/components/schemas/DeploymentSpec"}}},
		},
	}, map[string]map[string]interface{}{
		"DeploymentSpec": {
			"type": "object",
			"properties": map[string]interface{}{
				"strategy": map[string]interface{}{
					"allOf":                       []interface{}{map[string]interface{}{"$ref": "#/components/schemas/DeploymentStrategy"}},
					"x-kubernetes-patch-strategy": "retainKeys",
				},
				"ports": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"targetPort": map[string]interface{}{"oneOf": intOrString, "format": "int-or-string"}},
					},
				},
				"template": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"spec": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{"containers": map[string]interface{}{"type": "array", "items": container}},
						},
					},
				},
			},
		},
		"DeploymentStrategy": {
			"type": "object",
			"properties": map[string]interface{}{
				"maxSurge":       map[string]interface{}{"oneOf": intOrString, "format": "int-or-string"},
				"maxUnavailable": map[string]interface{}{"oneOf": intOrString, "format": "int-or-string"},
				"trueUnion":      map[string]interface{}{"oneOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "integer"}, map[string]interface{}{"type": "boolean"}}},
			},
		},
	}, "#/components/schemas/")
	if err != nil || normalized.field == nil {
		t.Fatalf("scalar union normalization failed: normalized=%+v err=%v", normalized, err)
	}
	spec := schemaFieldProperty(normalized.field, "spec")
	strategy := schemaFieldProperty(spec, "strategy")
	ports := schemaFieldProperty(spec, "ports")
	targetPort := schemaFieldProperty(ports.Items, "targetPort")
	template := schemaFieldProperty(spec, "template")
	templateSpec := schemaFieldProperty(template, "spec")
	containers := schemaFieldProperty(templateSpec, "containers")
	containerField := containers.Items
	lifecyclePort := schemaFieldProperty(schemaFieldProperty(schemaFieldProperty(containerField, "lifecycle"), "httpGet"), "port")
	probePort := schemaFieldProperty(schemaFieldProperty(schemaFieldProperty(containerField, "readinessProbe"), "httpGet"), "port")
	env := schemaFieldProperty(containerField, "env")
	valueFrom := schemaFieldProperty(env.Items, "valueFrom")
	resourceFieldRef := schemaFieldProperty(valueFrom, "resourceFieldRef")
	divisor := schemaFieldProperty(resourceFieldRef, "divisor")
	maxSurge := schemaFieldProperty(strategy, "maxSurge")
	maxUnavailable := schemaFieldProperty(strategy, "maxUnavailable")
	if strategy == nil || targetPort == nil || lifecyclePort == nil || probePort == nil || divisor == nil || maxSurge == nil || maxUnavailable == nil {
		t.Fatalf("scalar union fields were not retained: spec=%+v strategy=%+v targetPort=%+v lifecycle=%+v probe=%+v divisor=%+v fallbacks=%+v", spec, strategy, targetPort, lifecyclePort, probePort, divisor, normalized.fallbacks)
	}
	for name, field := range map[string]*SchemaField{
		"targetPort":     targetPort,
		"lifecycle.port": lifecyclePort,
		"probe.port":     probePort,
		"divisor":        divisor,
		"maxSurge":       maxSurge,
		"maxUnavailable": maxUnavailable,
	} {
		if field.Type != "string" || len(field.ScalarTypes) != 2 || field.ScalarTypes[0] != "string" {
			t.Fatalf("%s did not retain normalized scalar variants: %+v", name, field)
		}
	}
	if divisor.Format != "quantity" || targetPort.Format != "int-or-string" || lifecyclePort.Format != "int-or-string" || probePort.Format != "int-or-string" || maxSurge.Format != "int-or-string" || maxUnavailable.Format != "int-or-string" {
		t.Fatalf("scalar union formats were not preserved: divisor=%q targetPort=%q lifecycle=%q probe=%q maxSurge=%q maxUnavailable=%q", divisor.Format, targetPort.Format, lifecyclePort.Format, probePort.Format, maxSurge.Format, maxUnavailable.Format)
	}
	for _, fallback := range normalized.fallbacks {
		switch fallback.Path {
		case "$.spec.strategy.maxSurge", "$.spec.strategy.maxUnavailable", "$.spec.ports.items.targetPort", "$.spec.template.spec.containers.items.lifecycle.httpGet.port", "$.spec.template.spec.containers.items.readinessProbe.httpGet.port", "$.spec.template.spec.containers.items.env.items.valueFrom.resourceFieldRef.divisor", "$.spec.strategy":
			t.Fatalf("supported scalar or annotated allOf field used YAML fallback: %+v", fallback)
		case "$.spec.strategy.trueUnion":
			continue
		}
	}
	foundUnionFallback := false
	for _, fallback := range normalized.fallbacks {
		if fallback.Path == "$.spec.strategy.trueUnion" {
			foundUnionFallback = true
		}
	}
	if !foundUnionFallback {
		t.Fatalf("genuine multi-branch union did not remain a YAML fallback: %+v", normalized.fallbacks)
	}
}

// TestNormalizeOpenAPISchemaSupportsExactScalarAnyOfUnions verifies only the supported primitive anyOf pairs enter the form model.
func TestNormalizeOpenAPISchemaSupportsExactScalarAnyOfUnions(t *testing.T) {
	normalized, err := normalizeOpenAPISchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"port": map[string]interface{}{
						"anyOf":  []interface{}{map[string]interface{}{"type": "integer"}, map[string]interface{}{"type": "string"}},
						"format": "int-or-string",
					},
					"quantity": map[string]interface{}{
						"anyOf":  []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "number"}},
						"format": "quantity",
					},
					"objectOrArray": map[string]interface{}{
						"anyOf": []interface{}{map[string]interface{}{"type": "object"}, map[string]interface{}{"type": "array"}},
					},
					"multiBranch": map[string]interface{}{
						"anyOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "integer"}, map[string]interface{}{"type": "number"}},
					},
				},
			},
		},
	}, nil, "#/components/schemas/")
	if err != nil || normalized.field == nil {
		t.Fatalf("scalar anyOf normalization failed: normalized=%+v err=%v", normalized, err)
	}
	spec := schemaFieldProperty(normalized.field, "spec")
	port := schemaFieldProperty(spec, "port")
	quantity := schemaFieldProperty(spec, "quantity")
	if port == nil || port.Type != "string" || len(port.ScalarTypes) != 2 || port.ScalarTypes[0] != "string" || port.ScalarTypes[1] != "integer" || port.Format != "int-or-string" {
		t.Fatalf("string/integer anyOf was not normalized safely: %+v", port)
	}
	if quantity == nil || quantity.Type != "string" || len(quantity.ScalarTypes) != 2 || quantity.ScalarTypes[0] != "string" || quantity.ScalarTypes[1] != "number" || quantity.Format != "quantity" {
		t.Fatalf("string/number anyOf was not normalized safely: %+v", quantity)
	}
	if schemaFieldProperty(spec, "objectOrArray") != nil || schemaFieldProperty(spec, "multiBranch") != nil {
		t.Fatalf("unsupported anyOf branches were guessed into the form: %+v", spec)
	}
	fallbackPaths := map[string]bool{}
	for _, fallback := range normalized.fallbacks {
		fallbackPaths[fallback.Path] = true
	}
	if fallbackPaths["$.spec.port"] || fallbackPaths["$.spec.quantity"] || !fallbackPaths["$.spec.objectOrArray"] || !fallbackPaths["$.spec.multiBranch"] {
		t.Fatalf("scalar anyOf fallback coverage was incorrect: %+v", normalized.fallbacks)
	}
}

// TestNormalizeOpenAPISchemaResolvesBareDefinitionReferences verifies both OpenAPI reference prefixes resolve extracted definition names.
func TestNormalizeOpenAPISchemaResolvesBareDefinitionReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		prefix string
	}{
		{name: "v3", prefix: "#/components/schemas/"},
		{name: "v2", prefix: "#/definitions/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := normalizeOpenAPISchema(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"spec": map[string]interface{}{"allOf": []interface{}{map[string]interface{}{"$ref": test.prefix + "BareSpec"}}},
				},
			}, map[string]map[string]interface{}{
				"BareSpec": {"type": "object", "properties": map[string]interface{}{"mode": map[string]interface{}{"type": "string"}}},
			}, test.prefix)
			if err != nil || normalized.field == nil || schemaFieldProperty(schemaFieldProperty(normalized.field, "spec"), "mode") == nil {
				t.Fatalf("bare %s definition reference did not resolve: normalized=%+v err=%v", test.name, normalized, err)
			}
		})
	}
}

// TestNormalizeOpenAPISchemaLeavesSchemaUnionsAsYAMLFallback verifies multi-branch allOf and oneOf are never guessed into form controls.
func TestNormalizeOpenAPISchemaLeavesSchemaUnionsAsYAMLFallback(t *testing.T) {
	normalized, err := normalizeOpenAPISchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mode":   map[string]interface{}{"type": "string"},
					"union":  map[string]interface{}{"allOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "integer"}}},
					"choice": map[string]interface{}{"oneOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "integer"}, map[string]interface{}{"type": "boolean"}}},
				},
			},
		},
	}, nil, "#/components/schemas/")
	if err != nil || normalized.field == nil {
		t.Fatalf("union normalization failed unexpectedly: normalized=%+v err=%v", normalized, err)
	}
	spec := schemaFieldProperty(normalized.field, "spec")
	if spec == nil || schemaFieldProperty(spec, "mode") == nil || schemaFieldProperty(spec, "union") != nil || schemaFieldProperty(spec, "choice") != nil {
		t.Fatalf("union branches were guessed into the form: field=%+v", spec)
	}
	fallbackPaths := map[string]bool{}
	for _, fallback := range normalized.fallbacks {
		fallbackPaths[fallback.Path] = true
	}
	if !fallbackPaths["$.spec.union"] || !fallbackPaths["$.spec.choice"] {
		t.Fatalf("union fields did not remain explicit YAML fallbacks: %+v", normalized.fallbacks)
	}
}

// TestLoadResourceSchemaFallsBackToOpenAPIV2 verifies the compatibility document is used when v3 is unavailable.
func TestLoadResourceSchemaFallsBackToOpenAPIV2(t *testing.T) {
	v2Schema := &openapi_v2.Schema{
		Type:        &openapi_v2.TypeItem{Value: []string{"object"}},
		Description: "A compatibility schema.",
		Properties: &openapi_v2.Properties{AdditionalProperties: []*openapi_v2.NamedSchema{{
			Name: "spec",
			Value: &openapi_v2.Schema{
				Type:     &openapi_v2.TypeItem{Value: []string{"object"}},
				Required: []string{"mode"},
				Properties: &openapi_v2.Properties{AdditionalProperties: []*openapi_v2.NamedSchema{
					{Name: "mode", Value: &openapi_v2.Schema{Type: &openapi_v2.TypeItem{Value: []string{"string"}}, Default: &openapi_v2.Any{Yaml: "active"}, Description: "Compatibility mode."}},
					{Name: "targetPort", Value: &openapi_v2.Schema{Type: &openapi_v2.TypeItem{Value: []string{"string"}}, Format: "int-or-string"}},
				}},
			},
		}}},
	}
	discovery := testSchemaDiscovery(t, "Widget", nil, &openapi_v2.Document{Definitions: &openapi_v2.Definitions{AdditionalProperties: []*openapi_v2.NamedSchema{{Name: "io.k8s.api.example.v1.Widget", Value: v2Schema}}}})
	result, err := loadResourceSchema(context.Background(), fake.NewSimpleDynamicClient(runtime.NewScheme()), "example", "v1", "widgets", discovery)
	if err != nil || result.State != resourceSchemaStateAvailable || result.Source != resourceSchemaSourceOpenAPIV2 || result.Schema == nil {
		t.Fatalf("OpenAPI v2 fallback failed: result=%+v err=%v", result, err)
	}
	targetPort := schemaFieldProperty(schemaFieldProperty(result.Schema, "spec"), "targetPort")
	if result.Schema.Description != "A compatibility schema." || schemaFieldProperty(schemaFieldProperty(result.Schema, "spec"), "mode").Default != "active" || targetPort == nil || targetPort.Type != "string" || len(targetPort.ScalarTypes) != 2 || targetPort.ScalarTypes[0] != "string" || targetPort.ScalarTypes[1] != "integer" || targetPort.Format != "int-or-string" {
		t.Fatalf("v2 descriptions/defaults were not preserved: %+v", result.Schema)
	}
}

// TestSelectOpenAPISchemaRequiresExactGVK verifies version and kind mismatches cannot satisfy a schema request.
func TestSelectOpenAPISchemaRequiresExactGVK(t *testing.T) {
	definitions := map[string]map[string]interface{}{
		"io.k8s.api.apps.v1.Deployment":      {"type": "object", "x-kubernetes-group-version-kind": []interface{}{map[string]interface{}{"group": "apps", "version": "v1", "kind": "StatefulSet"}}},
		"io.k8s.api.apps.v1beta1.Deployment": {"type": "object", "x-kubernetes-group-version-kind": []interface{}{map[string]interface{}{"group": "apps", "version": "v1beta1", "kind": "Deployment"}}},
	}
	if _, found, err := selectOpenAPISchema(definitions, "apps", "v1", "Deployment"); err != nil || found {
		t.Fatalf("mismatched GVK was selected: found=%t err=%v", found, err)
	}
	definitions["custom"] = map[string]interface{}{"type": "object", "x-kubernetes-group-version-kind": []interface{}{map[string]interface{}{"group": "apps", "version": "v1", "kind": "Deployment"}}}
	selected, found, err := selectOpenAPISchema(definitions, "apps", "v1", "Deployment")
	if err != nil || !found || selected["type"] != "object" {
		t.Fatalf("exact extension GVK was not selected: selected=%+v found=%t err=%v", selected, found, err)
	}
	custom, found, err := selectOpenAPISchema(map[string]map[string]interface{}{"com.example.v1.Widget": {"type": "object"}}, "example.com", "v1", "Widget")
	if err != nil || !found || custom["type"] != "object" {
		t.Fatalf("reversed CRD model name was not selected: selected=%+v found=%t err=%v", custom, found, err)
	}
}

// TestNormalizeOpenAPISchemaBoundsRecursiveReferences verifies recursive refs become bounded field fallbacks.
func TestNormalizeOpenAPISchemaBoundsRecursiveReferences(t *testing.T) {
	definitions := map[string]map[string]interface{}{
		"#/components/schemas/Spec": {
			"type":        "object",
			"description": "Desired state.",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string"},
				"loop": map[string]interface{}{"$ref": "#/components/schemas/Spec"},
			},
		},
	}
	normalized, err := normalizeOpenAPISchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{"$ref": "#/components/schemas/Spec"},
		},
	}, definitions, "#/components/schemas/")
	if err != nil || normalized.field == nil || len(normalized.fallbacks) != 1 || normalized.fallbacks[0].Path != "$.spec.loop" {
		t.Fatalf("recursive ref was not converted to one bounded fallback: normalized=%+v err=%v", normalized, err)
	}
	if spec := schemaFieldProperty(normalized.field, "spec"); spec == nil || spec.Description != "Desired state." || schemaFieldProperty(spec, "name") == nil {
		t.Fatalf("supported fields around recursive ref were lost: %+v", normalized.field)
	}
}

// TestLoadResourceSchemaReportsOpenAPIStates verifies missing, malformed, unavailable, and RBAC outcomes remain explicit.
func TestLoadResourceSchemaReportsOpenAPIStates(t *testing.T) {
	tests := []struct {
		name        string
		discovery   resourceSchemaDiscovery
		state       string
		messagePart string
	}{
		{name: "missing", discovery: testSchemaDiscovery(t, "Deployment", nil, nil), state: resourceSchemaStateMissing, messagePart: "does not publish"},
		{name: "malformed", discovery: testSchemaDiscovery(t, "Deployment", map[string]interface{}{"broken": "not a schema"}, nil), state: resourceSchemaStateMalformed, messagePart: "malformed"},
		{name: "unavailable", discovery: testSchemaDiscoveryWithErrors(t, "Deployment", fmt.Errorf("connection refused"), nil), state: resourceSchemaStateUnavailable, messagePart: "unavailable"},
		{name: "permission denied", discovery: testSchemaDiscoveryWithErrors(t, "Deployment", apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "list", nil), nil), state: resourceSchemaStatePermissionDenied, messagePart: "RBAC"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := loadResourceSchema(context.Background(), fake.NewSimpleDynamicClient(runtime.NewScheme()), "apps", "v1", "deployments", test.discovery)
			if err != nil || result.State != test.state || !strings.Contains(result.Message, test.messagePart) {
				t.Fatalf("unexpected explicit state: result=%+v err=%v", result, err)
			}
		})
	}
}

// TestLoadResourceSchemaUsesYAMLFallbackForUnsupportedFields verifies a supported root keeps form fields beside union fallbacks.
func TestLoadResourceSchemaUsesYAMLFallbackForUnsupportedFields(t *testing.T) {
	discovery := testSchemaDiscovery(t, "Widget", map[string]interface{}{
		"io.k8s.api.example.v1.Widget": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"spec": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"mode":  map[string]interface{}{"type": "string"},
						"value": map[string]interface{}{"type": "string", "oneOf": []interface{}{map[string]interface{}{"type": "string"}}},
					},
				},
			},
		},
	}, nil)
	result, err := loadResourceSchema(context.Background(), fake.NewSimpleDynamicClient(runtime.NewScheme()), "example", "v1", "widgets", discovery)
	if err != nil || result.State != resourceSchemaStateAvailable || result.Schema == nil || len(result.Fallbacks) != 1 || result.Fallbacks[0].Path != "$.spec.value" || schemaFieldProperty(schemaFieldProperty(result.Schema, "spec"), "mode") == nil || schemaFieldProperty(schemaFieldProperty(result.Schema, "spec"), "value") != nil {
		t.Fatalf("unsupported field did not remain a YAML fallback: result=%+v err=%v", result, err)
	}
}

// TestLoadResourceSchemaNormalizesPreservedUnknownFields verifies preserved CRD data becomes a bounded string map without weakening status locks.
func TestLoadResourceSchemaNormalizesPreservedUnknownFields(t *testing.T) {
	crd := testCRD(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"spec": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"mode": map[string]interface{}{"type": "string"},
						},
						"additionalProperties": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{"nested": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
						},
						"x-kubernetes-preserve-unknown-fields": true,
					},
				},
			},
			"status": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"inputs": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"transportOpts": map[string]interface{}{
									"x-kubernetes-preserve-unknown-fields": true,
								},
							},
						},
					},
				},
			},
		},
	})
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), crd)
	result, err := loadResourceSchema(context.Background(), client, "widgets.example.com", "v1", "widgets")
	if err != nil || result.State != resourceSchemaStateAvailable || result.Schema == nil {
		t.Fatalf("preserved-unknown schema did not remain available: result=%+v err=%v", result, err)
	}
	if len(result.Fallbacks) != 0 || result.Message != "" {
		t.Fatalf("preserved-unknown fields unexpectedly used YAML fallback: message=%q fallbacks=%+v", result.Message, result.Fallbacks)
	}
	spec := schemaFieldProperty(result.Schema, "spec")
	config := schemaFieldProperty(spec, "config")
	mode := schemaFieldProperty(config, "mode")
	if config == nil || config.Type != "object" || mode == nil || mode.Type != "string" || config.AdditionalProperties == nil || config.AdditionalProperties.Type != "string" || config.AdditionalProperties.Properties != nil || config.AdditionalProperties.Items != nil {
		t.Fatalf("preserved-unknown field was not normalized to a string map: config=%+v mode=%+v", config, mode)
	}
	status := schemaFieldProperty(result.Schema, "status")
	inputs := schemaFieldProperty(status, "inputs")
	transportOpts := schemaFieldProperty(inputs.Items, "transportOpts")
	if status == nil || !status.ReadOnly || inputs == nil || !inputs.ReadOnly || inputs.Items == nil || !inputs.Items.ReadOnly || transportOpts == nil || transportOpts.Type != "object" || !transportOpts.ReadOnly || transportOpts.AdditionalProperties == nil || transportOpts.AdditionalProperties.Type != "string" || !transportOpts.AdditionalProperties.ReadOnly {
		t.Fatalf("status safety was not preserved around the normalized map: status=%+v inputs=%+v transportOpts=%+v", status, inputs, transportOpts)
	}
}

// TestResourceSchemaCacheIsBoundedAndDetached verifies revision-scoped schema cache eviction and clone safety.
func TestResourceSchemaCacheIsBoundedAndDetached(t *testing.T) {
	service := &Service{revision: 1}
	firstKey := resourceSchemaCacheKey{contextName: "field", revision: 1, group: "apps", version: "v1", resource: "resource-0"}
	for index := 0; index < resourceSchemaCacheMaxEntries+1; index++ {
		key := resourceSchemaCacheKey{contextName: "field", revision: 1, group: "apps", version: "v1", resource: fmt.Sprintf("resource-%d", index)}
		service.storeResourceSchema(key, ResourceSchema{State: resourceSchemaStateAvailable, Schema: &SchemaField{Type: "object", Properties: []SchemaField{{Name: "spec", Type: "object"}}}})
	}
	if _, found := service.cachedResourceSchema(firstKey); found {
		t.Fatal("oldest schema cache entry was not evicted")
	}
	lastKey := resourceSchemaCacheKey{contextName: "field", revision: 1, group: "apps", version: "v1", resource: fmt.Sprintf("resource-%d", resourceSchemaCacheMaxEntries)}
	cached, found := service.cachedResourceSchema(lastKey)
	if !found || cached.Schema == nil {
		t.Fatalf("newest schema cache entry was not retained: %+v found=%t", cached, found)
	}
	cached.Schema.Properties[0].Name = "mutated"
	uncopied, found := service.cachedResourceSchema(lastKey)
	if !found || uncopied.Schema.Properties[0].Name != "spec" {
		t.Fatalf("schema cache returned shared mutable data: %+v found=%t", uncopied, found)
	}
}

// schemaDiscoveryFixture provides deterministic resource discovery and OpenAPI document responses for schema tests.
type schemaDiscoveryFixture struct {
	resources   *metav1.APIResourceList
	resourceErr error
	v3          openapi.Client
	v2          *openapi_v2.Document
	v2Err       error
}

// ServerResourcesForGroupVersion returns the fixture's exact top-level resource list.
func (f *schemaDiscoveryFixture) ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error) {
	return f.resources, f.resourceErr
}

// OpenAPIV3 returns the fixture's version-specific OpenAPI client.
func (f *schemaDiscoveryFixture) OpenAPIV3() openapi.Client {
	return f.v3
}

// OpenAPISchema returns the fixture's compatibility OpenAPI v2 document.
func (f *schemaDiscoveryFixture) OpenAPISchema() (*openapi_v2.Document, error) {
	return f.v2, f.v2Err
}

// schemaOpenAPIClient serves one raw version-specific document through the client-go OpenAPI interface.
type schemaOpenAPIClient struct {
	paths map[string]openapi.GroupVersion
	err   error
}

// Paths returns deterministic version-specific OpenAPI paths.
func (c *schemaOpenAPIClient) Paths() (map[string]openapi.GroupVersion, error) {
	return c.paths, c.err
}

// schemaOpenAPIGroupVersion stores one JSON OpenAPI document for a fake version path.
type schemaOpenAPIGroupVersion struct {
	body []byte
	err  error
}

// Schema returns the fake version-specific OpenAPI JSON regardless of requested content type.
func (g *schemaOpenAPIGroupVersion) Schema(string) ([]byte, error) {
	return g.body, g.err
}

// ServerRelativeURL returns the stable fake schema URL used only for the client cache key.
func (g *schemaOpenAPIGroupVersion) ServerRelativeURL() string {
	return "/openapi/v3/schema-test"
}

// testSchemaDiscovery builds a fixture with either a v3 component map or a v2 compatibility document.
func testSchemaDiscovery(t *testing.T, kind string, schemas map[string]interface{}, v2 *openapi_v2.Document) *schemaDiscoveryFixture {
	return testSchemaDiscoveryWithErrors(t, kind, nil, v2, schemas)
}

// testSchemaDiscoveryWithErrors builds a fixture with explicit v3 retrieval and v2 document behavior.
func testSchemaDiscoveryWithErrors(t *testing.T, kind string, v3Err error, v2 *openapi_v2.Document, schemas ...map[string]interface{}) *schemaDiscoveryFixture {
	t.Helper()
	resource := strings.ToLower(kind) + "s"
	fixture := &schemaDiscoveryFixture{
		resources: &metav1.APIResourceList{APIResources: []metav1.APIResource{{Name: resource, Kind: kind}}},
		v2:        v2,
	}
	if v3Err != nil {
		fixture.v3 = &schemaOpenAPIClient{err: v3Err}
		return fixture
	}
	paths := map[string]openapi.GroupVersion{}
	if len(schemas) > 0 && schemas[0] != nil {
		document := map[string]interface{}{"openapi": "3.0.0", "components": map[string]interface{}{"schemas": schemas[0]}}
		body, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("marshal fake OpenAPI v3 document: %v", err)
		}
		for _, path := range []string{"apis/apps/v1", "apis/example/v1", "api/v1"} {
			paths[path] = &schemaOpenAPIGroupVersion{body: body}
		}
	}
	fixture.v3 = &schemaOpenAPIClient{paths: paths}
	return fixture
}

// schemaFieldProperty returns one named normalized child field.
func schemaFieldProperty(field *SchemaField, name string) *SchemaField {
	if field == nil {
		return nil
	}
	for index := range field.Properties {
		if field.Properties[index].Name == name {
			return &field.Properties[index]
		}
	}
	return nil
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
