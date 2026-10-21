package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	resourceSchemaStateAvailable        = "available"
	resourceSchemaStateUnsupported      = "unsupported"
	resourceSchemaStateMissing          = "missing"
	resourceSchemaStateNonStructural    = "non-structural"
	resourceSchemaStatePermissionDenied = "permission-denied"
	resourceSchemaStateError            = "error"
	resourceSchemaMaxDepth              = 32
	resourceSchemaMaxFields             = 512
)

// ResourceSchema is the safe schema response used by the browser form and YAML fallback.
type ResourceSchema struct {
	State    string       `json:"state"`
	Message  string       `json:"message,omitempty"`
	Group    string       `json:"group"`
	Version  string       `json:"version"`
	Resource string       `json:"resource"`
	Kind     string       `json:"kind,omitempty"`
	Schema   *SchemaField `json:"schema,omitempty"`
}

// SchemaField is a bounded, browser-safe subset of a structural OpenAPI v3 field.
type SchemaField struct {
	Name                 string        `json:"name,omitempty"`
	Type                 string        `json:"type"`
	Title                string        `json:"title,omitempty"`
	Description          string        `json:"description,omitempty"`
	Format               string        `json:"format,omitempty"`
	Required             bool          `json:"required,omitempty"`
	ReadOnly             bool          `json:"readOnly,omitempty"`
	Enum                 []interface{} `json:"enum,omitempty"`
	Default              interface{}   `json:"default,omitempty"`
	Properties           []SchemaField `json:"properties,omitempty"`
	Items                *SchemaField  `json:"items,omitempty"`
	AdditionalProperties *SchemaField  `json:"additionalProperties,omitempty"`
	Minimum              *float64      `json:"minimum,omitempty"`
	ExclusiveMinimum     bool          `json:"exclusiveMinimum,omitempty"`
	Maximum              *float64      `json:"maximum,omitempty"`
	ExclusiveMaximum     bool          `json:"exclusiveMaximum,omitempty"`
	MultipleOf           *float64      `json:"multipleOf,omitempty"`
	MinLength            *int64        `json:"minLength,omitempty"`
	MaxLength            *int64        `json:"maxLength,omitempty"`
	Pattern              string        `json:"pattern,omitempty"`
	MinItems             *int64        `json:"minItems,omitempty"`
	MaxItems             *int64        `json:"maxItems,omitempty"`
	UniqueItems          bool          `json:"uniqueItems,omitempty"`
}

// ResourceSchema returns the structural schema for a discovered custom resource.
func (s *Service) ResourceSchema(ctx context.Context, contextName, group, version, resource string) (ResourceSchema, error) {
	group = strings.TrimSpace(group)
	version = strings.TrimSpace(version)
	resource = strings.TrimSpace(resource)
	if version == "" || resource == "" {
		return ResourceSchema{}, fmt.Errorf("version and resource are required for schema discovery")
	}
	result := ResourceSchema{Group: group, Version: version, Resource: resource}
	if group == "" || group == "apiextensions.k8s.io" {
		result.State = resourceSchemaStateUnsupported
		result.Message = "Schema forms are available for custom resources only; use the YAML editor for this resource."
		return result, nil
	}

	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return ResourceSchema{}, err
	}
	return loadResourceSchema(ctx, dynamicClient, group, version, resource)
}

// loadResourceSchema reads one CustomResourceDefinition through the generic dynamic API.
func loadResourceSchema(ctx context.Context, client dynamic.Interface, group, version, resource string) (ResourceSchema, error) {
	result := ResourceSchema{Group: group, Version: version, Resource: resource}
	crdName := resource + "." + group
	crd, err := client.Resource(customResourceDefinitionGVR()).Get(ctx, crdName, metav1.GetOptions{})
	if err != nil {
		switch {
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			result.State = resourceSchemaStatePermissionDenied
			result.Message = "Kubernetes RBAC denied access to the CustomResourceDefinition schema."
			return result, nil
		case apierrors.IsNotFound(err):
			result.State = resourceSchemaStateMissing
			result.Message = "The CustomResourceDefinition was not found; use the YAML editor for this resource."
			return result, nil
		default:
			result.State = resourceSchemaStateError
			result.Message = "The CustomResourceDefinition schema could not be read."
			return result, nil
		}
	}

	field, state, message := parseCRDResourceSchema(crd, group, version, resource)
	result.State = state
	result.Message = message
	if field != nil {
		result.Schema = field
		result.Kind = crdKind(crd)
	}
	return result, nil
}

// markCustomResourceDefinitions annotates discovered definitions without making CRDs a typed resource implementation.
func markCustomResourceDefinitions(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition) {
	_ = markCustomResourceDefinitionsWithError(ctx, client, definitions)
}

// markCustomResourceDefinitionsWithError annotates all discovered CRD versions and reports metadata access failures.
func markCustomResourceDefinitionsWithError(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition) error {
	if client == nil || len(definitions) == 0 {
		return nil
	}
	resourceClient := client.Resource(customResourceDefinitionGVR())
	continueToken := ""
	seenContinueTokens := make(map[string]struct{})
	for {
		list, err := resourceClient.List(ctx, metav1.ListOptions{Continue: continueToken})
		if err != nil {
			return err
		}
		for index := range list.Items {
			crd := &list.Items[index]
			group, foundGroup, groupErr := unstructured.NestedString(crd.Object, "spec", "group")
			plural, foundPlural, pluralErr := unstructured.NestedString(crd.Object, "spec", "names", "plural")
			if groupErr != nil || pluralErr != nil || !foundGroup || !foundPlural {
				continue
			}
			versions, foundVersions, versionsErr := unstructured.NestedSlice(crd.Object, "spec", "versions")
			if versionsErr != nil || !foundVersions {
				continue
			}
			for _, rawVersion := range versions {
				versionObject, ok := rawVersion.(map[string]interface{})
				if !ok {
					continue
				}
				versionName, foundName, nameErr := unstructured.NestedString(versionObject, "name")
				if nameErr != nil || !foundName {
					continue
				}
				for definitionIndex := range definitions {
					definition := &definitions[definitionIndex]
					if definition.Group == group && definition.Version == versionName && definition.Resource == plural {
						definition.CustomResource = true
					}
				}
			}
		}
		continueToken = list.GetContinue()
		if continueToken == "" {
			return nil
		}
		if _, found := seenContinueTokens[continueToken]; found {
			return fmt.Errorf("CustomResourceDefinition discovery returned a repeated continuation token")
		}
		seenContinueTokens[continueToken] = struct{}{}
	}
}

// customResourceDefinitionGVR returns the native dynamic coordinates for CRD objects.
func customResourceDefinitionGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
}

// parseCRDResourceSchema selects and normalizes the requested served CRD version.
func parseCRDResourceSchema(crd *unstructured.Unstructured, group, version, resource string) (*SchemaField, string, string) {
	if crd == nil {
		return nil, resourceSchemaStateMissing, "The CustomResourceDefinition was not returned by Kubernetes; use the YAML editor for this resource."
	}
	crdGroup, foundGroup, groupErr := unstructured.NestedString(crd.Object, "spec", "group")
	crdPlural, foundPlural, pluralErr := unstructured.NestedString(crd.Object, "spec", "names", "plural")
	if groupErr != nil || pluralErr != nil || !foundGroup || !foundPlural || crdGroup != group || crdPlural != resource {
		return nil, resourceSchemaStateMissing, "The discovered resource does not have a matching CustomResourceDefinition schema."
	}
	versions, foundVersions, versionsErr := unstructured.NestedSlice(crd.Object, "spec", "versions")
	if versionsErr != nil || !foundVersions {
		return nil, resourceSchemaStateNonStructural, "This CustomResourceDefinition does not expose versioned OpenAPI v3 schemas."
	}
	for _, rawVersion := range versions {
		versionObject, ok := rawVersion.(map[string]interface{})
		if !ok {
			continue
		}
		versionName, foundName, nameErr := unstructured.NestedString(versionObject, "name")
		if nameErr != nil || !foundName || versionName != version {
			continue
		}
		versionSchema, foundSchema, schemaErr := unstructured.NestedMap(versionObject, "schema", "openAPIV3Schema")
		if schemaErr != nil {
			return nil, resourceSchemaStateUnsupported, "The CRD OpenAPI v3 schema is malformed; use the YAML editor."
		}
		if !foundSchema || len(versionSchema) == 0 {
			return nil, resourceSchemaStateNonStructural, "This CRD version has no structural OpenAPI v3 schema; use the YAML editor."
		}
		field, err := parseSchemaField(versionSchema, "", false, 0, new(int))
		if err != nil {
			return nil, resourceSchemaStateUnsupported, fmt.Sprintf("This CRD schema cannot be represented safely as a form: %v", err)
		}
		return field, resourceSchemaStateAvailable, ""
	}
	return nil, resourceSchemaStateMissing, fmt.Sprintf("The CRD does not serve version %q; use the YAML editor.", version)
}

// crdKind returns the kind declared by a CustomResourceDefinition without reading custom resources.
func crdKind(crd *unstructured.Unstructured) string {
	if crd == nil {
		return ""
	}
	kind, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "kind")
	return kind
}

// parseSchemaField converts one structural OpenAPI v3 object into the bounded form model.
func parseSchemaField(raw map[string]interface{}, name string, required bool, depth int, count *int) (*SchemaField, error) {
	if depth > resourceSchemaMaxDepth {
		return nil, fmt.Errorf("schema nesting exceeds %d levels", resourceSchemaMaxDepth)
	}
	if count == nil {
		return nil, fmt.Errorf("schema field counter is unavailable")
	}
	*count++
	if *count > resourceSchemaMaxFields {
		return nil, fmt.Errorf("schema contains more than %d fields", resourceSchemaMaxFields)
	}
	if raw == nil {
		return nil, fmt.Errorf("schema field %q is empty", name)
	}
	if name != "" && !safeSchemaPropertyName(name) {
		return nil, fmt.Errorf("schema property name %q is unsafe", name)
	}
	if unsupported, keyword := hasUnsupportedSchemaKeyword(raw); unsupported {
		return nil, fmt.Errorf("schema uses unsupported construct %q", keyword)
	}
	typeName, foundType, typeErr := unstructured.NestedString(raw, "type")
	if typeErr != nil || !foundType || typeName == "" {
		return nil, fmt.Errorf("schema field %q does not declare a type", name)
	}
	if !supportedSchemaType(typeName) {
		return nil, fmt.Errorf("schema field %q uses unsupported type %q", name, typeName)
	}

	field := &SchemaField{Name: name, Type: typeName, Required: required}
	field.Title, _, _ = unstructured.NestedString(raw, "title")
	field.Description, _, _ = unstructured.NestedString(raw, "description")
	field.Format, _, _ = unstructured.NestedString(raw, "format")
	field.ReadOnly, _, _ = unstructured.NestedBool(raw, "readOnly")
	if defaultValue, foundDefault := raw["default"]; foundDefault {
		normalized, ok := normalizeSchemaJSON(defaultValue)
		if !ok {
			return nil, fmt.Errorf("schema field %q has an unsupported default value", name)
		}
		field.Default = normalized
	}

	var err error
	field.Minimum, err = schemaFloatConstraint(raw, "minimum")
	if err != nil {
		return nil, fmt.Errorf("schema field %q minimum: %w", name, err)
	}
	field.Maximum, err = schemaFloatConstraint(raw, "maximum")
	if err != nil {
		return nil, fmt.Errorf("schema field %q maximum: %w", name, err)
	}
	field.MultipleOf, err = schemaFloatConstraint(raw, "multipleOf")
	if err != nil {
		return nil, fmt.Errorf("schema field %q multipleOf: %w", name, err)
	}
	field.MinLength, err = schemaIntegerConstraint(raw, "minLength")
	if err != nil {
		return nil, fmt.Errorf("schema field %q minLength: %w", name, err)
	}
	field.MaxLength, err = schemaIntegerConstraint(raw, "maxLength")
	if err != nil {
		return nil, fmt.Errorf("schema field %q maxLength: %w", name, err)
	}
	field.MinItems, err = schemaIntegerConstraint(raw, "minItems")
	if err != nil {
		return nil, fmt.Errorf("schema field %q minItems: %w", name, err)
	}
	field.MaxItems, err = schemaIntegerConstraint(raw, "maxItems")
	if err != nil {
		return nil, fmt.Errorf("schema field %q maxItems: %w", name, err)
	}
	field.Pattern, _, _ = unstructured.NestedString(raw, "pattern")
	if field.Pattern != "" {
		if _, err := regexp.Compile(field.Pattern); err != nil {
			return nil, fmt.Errorf("schema field %q has an invalid pattern", name)
		}
	}
	field.ExclusiveMinimum, _, _ = unstructured.NestedBool(raw, "exclusiveMinimum")
	field.ExclusiveMaximum, _, _ = unstructured.NestedBool(raw, "exclusiveMaximum")
	field.UniqueItems, _, _ = unstructured.NestedBool(raw, "uniqueItems")
	field.Enum, err = schemaEnumValues(raw["enum"], typeName)
	if err != nil {
		return nil, fmt.Errorf("schema field %q enum: %w", name, err)
	}

	switch typeName {
	case "object":
		if err := populateObjectSchema(field, raw, depth, count); err != nil {
			return nil, err
		}
	case "array":
		items, foundItems, itemsErr := unstructured.NestedMap(raw, "items")
		if itemsErr != nil || !foundItems {
			return nil, fmt.Errorf("schema array field %q has no item schema", name)
		}
		field.Items, err = parseSchemaField(items, "", false, depth+1, count)
		if err != nil {
			return nil, err
		}
	}
	return field, nil
}

// populateObjectSchema parses named properties and typed map values for an object field.
func populateObjectSchema(field *SchemaField, raw map[string]interface{}, depth int, count *int) error {
	requiredNames, err := requiredSchemaNames(raw["required"])
	if err != nil {
		return fmt.Errorf("object field %q required list: %w", field.Name, err)
	}
	properties, foundProperties, propertiesErr := unstructured.NestedMap(raw, "properties")
	if propertiesErr != nil {
		return fmt.Errorf("object field %q properties: %w", field.Name, propertiesErr)
	}
	if foundProperties {
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			property, ok := properties[name].(map[string]interface{})
			if !ok {
				return fmt.Errorf("object field %q property %q is malformed", field.Name, name)
			}
			child, childErr := parseSchemaField(property, name, requiredNames[name], depth+1, count)
			if childErr != nil {
				return childErr
			}
			field.Properties = append(field.Properties, *child)
		}
	}
	if len(requiredNames) > 0 {
		for name := range requiredNames {
			if _, found := properties[name]; !found {
				return fmt.Errorf("object field %q requires unknown property %q", field.Name, name)
			}
		}
	}
	if additional, foundAdditional := raw["additionalProperties"]; foundAdditional {
		if additional == nil {
			return nil
		}
		if allowed, isBool := additional.(bool); isBool {
			if allowed {
				return fmt.Errorf("object field %q allows untyped additional properties", field.Name)
			}
			return nil
		}
		additionalMap, ok := additional.(map[string]interface{})
		if !ok {
			return fmt.Errorf("object field %q additional properties are malformed", field.Name)
		}
		field.AdditionalProperties, err = parseSchemaField(additionalMap, "", false, depth+1, count)
		if err != nil {
			return err
		}
	}
	return nil
}

// hasUnsupportedSchemaKeyword identifies schema constructs the browser form cannot safely model.
func hasUnsupportedSchemaKeyword(raw map[string]interface{}) (bool, string) {
	for _, keyword := range []string{"$ref", "oneOf", "anyOf", "allOf", "not", "if", "then", "else", "contains", "patternProperties", "dependencies", "dependentRequired", "additionalItems"} {
		if value, found := raw[keyword]; found && value != nil {
			return true, keyword
		}
	}
	if preserved, found := raw["x-kubernetes-preserve-unknown-fields"]; found {
		if value, ok := preserved.(bool); ok && value {
			return true, "x-kubernetes-preserve-unknown-fields"
		}
	}
	if intOrString, found := raw["x-kubernetes-int-or-string"]; found {
		if value, ok := intOrString.(bool); ok && value {
			return true, "x-kubernetes-int-or-string"
		}
	}
	return false, ""
}

// supportedSchemaType reports whether a JSON Schema type has a safe native form control.
func supportedSchemaType(typeName string) bool {
	switch typeName {
	case "string", "integer", "number", "boolean", "object", "array":
		return true
	default:
		return false
	}
}

// safeSchemaPropertyName rejects names that could change browser object prototypes during form edits.
func safeSchemaPropertyName(name string) bool {
	if name == "__proto__" || name == "prototype" || name == "constructor" {
		return false
	}
	return name != "" && len(name) <= 253 && !strings.ContainsAny(name, "\x00\r\n")
}

// requiredSchemaNames converts the JSON Schema required array into a lookup set.
func requiredSchemaNames(value interface{}) (map[string]bool, error) {
	result := map[string]bool{}
	if value == nil {
		return result, nil
	}
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	for _, item := range items {
		name, ok := item.(string)
		if !ok || !safeSchemaPropertyName(name) {
			return nil, fmt.Errorf("contains an unsafe property name")
		}
		result[name] = true
	}
	return result, nil
}

// schemaEnumValues normalizes primitive enum values and verifies they match the field type.
func schemaEnumValues(value interface{}, typeName string) ([]interface{}, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	result := make([]interface{}, 0, len(items))
	for _, item := range items {
		normalized, ok := normalizeSchemaJSON(item)
		if !ok || !schemaValueMatchesType(normalized, typeName) {
			return nil, fmt.Errorf("contains a value that does not match %q", typeName)
		}
		if _, duplicate := findSchemaEnum(result, normalized); duplicate {
			continue
		}
		result = append(result, normalized)
	}
	return result, nil
}

// findSchemaEnum checks whether one normalized enum value already exists.
func findSchemaEnum(values []interface{}, candidate interface{}) (int, bool) {
	for index, value := range values {
		if fmt.Sprintf("%v", value) == fmt.Sprintf("%v", candidate) {
			return index, true
		}
	}
	return -1, false
}

// schemaValueMatchesType verifies normalized JSON data against a supported schema type.
func schemaValueMatchesType(value interface{}, typeName string) bool {
	switch typeName {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := schemaNumber(value)
		return ok && math.Trunc(number) == number
	case "number":
		_, ok := schemaNumber(value)
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	default:
		return false
	}
}

// normalizeSchemaJSON copies only JSON-compatible schema values into response-safe values.
func normalizeSchemaJSON(value interface{}) (interface{}, bool) {
	switch typed := value.(type) {
	case nil, string, bool:
		return typed, true
	case float32:
		if math.IsNaN(float64(typed)) || math.IsInf(float64(typed), 0) {
			return nil, false
		}
		return float64(typed), true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return nil, false
		}
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, false
		}
		return number, true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, item := range typed {
			normalized, ok := normalizeSchemaJSON(item)
			if !ok {
				return nil, false
			}
			result[index] = normalized
		}
		return result, true
	case map[string]interface{}:
		result := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			normalized, ok := normalizeSchemaJSON(item)
			if !ok {
				return nil, false
			}
			result[key] = normalized
		}
		return result, true
	default:
		return nil, false
	}
}

// schemaNumber converts supported JSON numeric values into finite float64 values.
func schemaNumber(value interface{}) (float64, bool) {
	normalized, ok := normalizeSchemaJSON(value)
	if !ok {
		return 0, false
	}
	number, ok := normalized.(float64)
	return number, ok && !math.IsNaN(number) && !math.IsInf(number, 0)
}

// schemaFloatConstraint reads an optional finite numeric constraint.
func schemaFloatConstraint(raw map[string]interface{}, key string) (*float64, error) {
	value, found := raw[key]
	if !found {
		return nil, nil
	}
	number, ok := schemaNumber(value)
	if !ok {
		return nil, fmt.Errorf("must be a finite number")
	}
	if key == "multipleOf" && number <= 0 {
		return nil, fmt.Errorf("must be greater than zero")
	}
	return &number, nil
}

// schemaIntegerConstraint reads an optional non-negative integer constraint.
func schemaIntegerConstraint(raw map[string]interface{}, key string) (*int64, error) {
	value, found := raw[key]
	if !found {
		return nil, nil
	}
	number, ok := schemaNumber(value)
	if !ok || math.Trunc(number) != number || number < 0 || number > math.MaxInt64 {
		return nil, fmt.Errorf("must be a non-negative integer")
	}
	integer := int64(number)
	return &integer, nil
}
