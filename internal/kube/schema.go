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
	resourceSchemaMaxFallbacks          = 32
)

// ResourceSchema is the safe schema response used by the browser form and YAML fallback.
type ResourceSchema struct {
	State              string           `json:"state"`
	Message            string           `json:"message,omitempty"`
	Group              string           `json:"group"`
	Version            string           `json:"version"`
	Resource           string           `json:"resource"`
	Kind               string           `json:"kind,omitempty"`
	Schema             *SchemaField     `json:"schema,omitempty"`
	Fallbacks          []SchemaFallback `json:"fallbacks,omitempty"`
	FallbacksTruncated bool             `json:"fallbacksTruncated,omitempty"`
	StatusSubresource  bool             `json:"statusSubresource,omitempty"`
	ScaleStatusPath    string           `json:"scaleStatusPath,omitempty"`
}

// SchemaFallback explains one bounded schema field that remains available through YAML.
type SchemaFallback struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
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

	result.Kind = crdKind(crd)
	parsed := parseCRDResourceSchemaDetails(crd, group, version, resource, true)
	result.State = parsed.state
	result.Message = parsed.message
	result.Fallbacks = parsed.fallbacks
	result.FallbacksTruncated = parsed.fallbacksTruncated
	result.StatusSubresource = parsed.statusSubresource
	result.ScaleStatusPath = parsed.scaleStatusPath
	if parsed.field != nil {
		result.Schema = parsed.field
	}
	return result, nil
}

// markCustomResourceDefinitions annotates discovered definitions without making CRDs a typed resource implementation.
func markCustomResourceDefinitions(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition) {
	_ = markCustomResourceDefinitionsWithError(ctx, client, definitions)
}

// markCustomResourceDefinitionsWithError annotates all discovered CRD versions and reports metadata access failures.
func markCustomResourceDefinitionsWithError(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, preferredVersionMaps ...map[string]string) error {
	if client == nil || len(definitions) == 0 {
		return nil
	}
	preferredVersions := map[string]string(nil)
	if len(preferredVersionMaps) > 0 {
		preferredVersions = preferredVersionMaps[0]
	}
	definitionIndexes := make(map[string][]int, len(definitions))
	for index, definition := range definitions {
		key := definition.Group + "\x00" + definition.Resource
		definitionIndexes[key] = append(definitionIndexes[key], index)
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
			capabilitiesByVersion, ok := customResourceCapabilitiesForCRD(crd, preferredVersions)
			if !ok {
				continue
			}
			group, plural := "", ""
			for _, capability := range capabilitiesByVersion {
				group, plural = capability.Group, capability.Resource
				break
			}
			for _, definitionIndex := range definitionIndexes[group+"\x00"+plural] {
				definition := &definitions[definitionIndex]
				capability, found := capabilitiesByVersion[definition.Version]
				if !found {
					continue
				}
				definition.CustomResource = true
				if capability.Scope == "" {
					capability = cloneCustomResourceCapabilities(capability)
					capability.Scope = definition.Scope
				}
				definition.CustomResourceCapabilities = cloneCustomResourceCapabilities(capability)
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
	parsed := parseCRDResourceSchemaDetails(crd, group, version, resource, false)
	return parsed.field, parsed.state, parsed.message
}

// parsedCRDResourceSchema carries normalized fields plus bounded fallback metadata.
type parsedCRDResourceSchema struct {
	field              *SchemaField
	state              string
	message            string
	fallbacks          []SchemaFallback
	fallbacksTruncated bool
	statusSubresource  bool
	scaleStatusPath    string
}

// parseCRDResourceSchemaDetails selects one served version and records safe form fallbacks.
func parseCRDResourceSchemaDetails(crd *unstructured.Unstructured, group, version, resource string, allowFieldFallbacks bool) parsedCRDResourceSchema {
	if crd == nil {
		return parsedCRDResourceSchema{state: resourceSchemaStateMissing, message: "The CustomResourceDefinition was not returned by Kubernetes; use the YAML editor for this resource."}
	}
	spec, foundSpec, specErr := unstructured.NestedMap(crd.Object, "spec")
	if specErr != nil || !foundSpec {
		return parsedCRDResourceSchema{state: resourceSchemaStateMissing, message: "The CustomResourceDefinition has no usable version metadata; use the YAML editor for this resource."}
	}
	crdGroup, foundGroup, groupErr := unstructured.NestedString(spec, "group")
	crdPlural, foundPlural, pluralErr := unstructured.NestedString(spec, "names", "plural")
	if groupErr != nil || pluralErr != nil || !foundGroup || !foundPlural || crdGroup != group || crdPlural != resource {
		return parsedCRDResourceSchema{state: resourceSchemaStateMissing, message: "The discovered resource does not have a matching CustomResourceDefinition schema."}
	}
	versions, foundVersions, versionsErr := unstructured.NestedSlice(spec, "versions")
	if versionsErr != nil || !foundVersions {
		return parsedCRDResourceSchema{state: resourceSchemaStateNonStructural, message: "This CustomResourceDefinition does not expose versioned OpenAPI v3 schemas."}
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
		served, foundServed, servedErr := unstructured.NestedBool(versionObject, "served")
		if servedErr != nil || !foundServed || !served {
			return parsedCRDResourceSchema{state: resourceSchemaStateMissing, message: fmt.Sprintf("The CRD does not serve version %q; use the YAML editor.", version)}
		}
		statusSubresource, _, scalePaths := crdSubresourceMetadata(spec)
		versionSchema, foundSchema, schemaErr := unstructured.NestedMap(versionObject, "schema", "openAPIV3Schema")
		if schemaErr != nil {
			return parsedCRDResourceSchema{state: resourceSchemaStateUnsupported, message: "The CRD OpenAPI v3 schema is malformed; use the YAML editor.", statusSubresource: statusSubresource, scaleStatusPath: scalePaths.statusReplicas}
		}
		if !foundSchema || len(versionSchema) == 0 {
			return parsedCRDResourceSchema{state: resourceSchemaStateNonStructural, message: "This CRD version has no structural OpenAPI v3 schema; use the YAML editor.", statusSubresource: statusSubresource, scaleStatusPath: scalePaths.statusReplicas}
		}
		fallbacks := make([]SchemaFallback, 0)
		fallbacksTruncated := false
		var field *SchemaField
		var err error
		if allowFieldFallbacks {
			field, err = parseSchemaFieldWithFallbacks(versionSchema, "", false, 0, new(int), &fallbacks, &fallbacksTruncated)
		} else {
			field, err = parseSchemaField(versionSchema, "", false, 0, new(int))
		}
		if err != nil {
			return parsedCRDResourceSchema{state: resourceSchemaStateUnsupported, message: fmt.Sprintf("This CRD schema cannot be represented safely as a form: %v", err), statusSubresource: statusSubresource, scaleStatusPath: scalePaths.statusReplicas}
		}
		if statusSubresource {
			markStatusSchemaFieldReadOnly(field)
		} else if scalePaths.statusReplicas != "" {
			markSchemaFieldReadOnlyPath(field, scalePaths.statusReplicas)
		}
		message := ""
		if len(fallbacks) > 0 {
			message = schemaFallbackMessage(fallbacks, fallbacksTruncated)
		}
		return parsedCRDResourceSchema{field: field, state: resourceSchemaStateAvailable, message: message, fallbacks: fallbacks, fallbacksTruncated: fallbacksTruncated, statusSubresource: statusSubresource, scaleStatusPath: scalePaths.statusReplicas}
	}
	return parsedCRDResourceSchema{state: resourceSchemaStateMissing, message: fmt.Sprintf("The CRD does not serve version %q; use the YAML editor.", version)}
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

// parseSchemaFieldWithFallbacks removes unsupported nested fields before strict normalization.
func parseSchemaFieldWithFallbacks(raw map[string]interface{}, name string, required bool, depth int, count *int, fallbacks *[]SchemaFallback, truncated *bool) (*SchemaField, error) {
	cleaned, err := stripUnsupportedSchemaFields(raw, "$", fallbacks, truncated)
	if err != nil {
		return nil, err
	}
	if cleaned == nil {
		return nil, nil
	}
	return parseSchemaField(cleaned, name, required, depth, count)
}

// stripUnsupportedSchemaFields preserves supported object properties while recording nested YAML fallbacks.
func stripUnsupportedSchemaFields(raw map[string]interface{}, path string, fallbacks *[]SchemaFallback, truncated *bool) (map[string]interface{}, error) {
	fields := 0
	return stripUnsupportedSchemaFieldsBounded(raw, path, fallbacks, truncated, 0, &fields)
}

// stripUnsupportedSchemaFieldsBounded recursively normalizes only within the same depth and field limits as form parsing.
func stripUnsupportedSchemaFieldsBounded(raw map[string]interface{}, path string, fallbacks *[]SchemaFallback, truncated *bool, depth int, fields *int) (map[string]interface{}, error) {
	if raw == nil {
		return nil, fmt.Errorf("schema field at %s is empty", path)
	}
	if depth > resourceSchemaMaxDepth {
		if path == "$" {
			return nil, fmt.Errorf("schema nesting exceeds %d levels", resourceSchemaMaxDepth)
		}
		appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("schema nesting exceeds %d levels", resourceSchemaMaxDepth))
		return nil, nil
	}
	if fields == nil {
		return nil, fmt.Errorf("schema field counter is unavailable")
	}
	*fields = *fields + 1
	if *fields > resourceSchemaMaxFields {
		if path == "$" {
			return nil, fmt.Errorf("schema contains more than %d fields", resourceSchemaMaxFields)
		}
		appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("schema contains more than %d fields", resourceSchemaMaxFields))
		return nil, nil
	}
	if unsupported, keyword := hasUnsupportedSchemaKeyword(raw); unsupported {
		if path == "$" {
			return nil, fmt.Errorf("schema uses unsupported construct %q", keyword)
		}
		appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("unsupported schema construct %q", keyword))
		return nil, nil
	}
	if required, found := raw["required"]; found {
		if requiredNames, ok := required.([]interface{}); ok && len(requiredNames) > resourceSchemaMaxFields {
			if path == "$" {
				return nil, fmt.Errorf("schema required list contains more than %d fields", resourceSchemaMaxFields)
			}
			appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("schema required list contains more than %d fields", resourceSchemaMaxFields))
			return nil, nil
		}
	}
	cleaned := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		cleaned[key] = value
	}

	if rawProperties, found := raw["properties"]; found {
		properties, ok := rawProperties.(map[string]interface{})
		if ok {
			availableFields := resourceSchemaMaxFields - *fields
			if len(properties) > availableFields {
				if path == "$" {
					return nil, fmt.Errorf("schema contains more than %d fields", resourceSchemaMaxFields)
				}
				appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("schema contains more than %d fields", resourceSchemaMaxFields))
				return nil, nil
			}
			cleanedProperties := make(map[string]interface{}, len(properties))
			propertyNames := make([]string, 0, len(properties))
			for propertyName := range properties {
				propertyNames = append(propertyNames, propertyName)
			}
			sort.Strings(propertyNames)
			for _, propertyName := range propertyNames {
				rawProperty := properties[propertyName]
				property, ok := rawProperty.(map[string]interface{})
				if !ok {
					cleanedProperties[propertyName] = rawProperty
					continue
				}
				child, err := stripUnsupportedSchemaFieldsBounded(property, schemaPath(path, propertyName), fallbacks, truncated, depth+1, fields)
				if err != nil {
					return nil, err
				}
				if child != nil {
					cleanedProperties[propertyName] = child
				}
			}
			cleaned["properties"] = cleanedProperties
			cleaned["required"] = filteredRequiredSchemaNames(raw["required"], cleanedProperties)
		}
	}

	if rawItems, found := raw["items"]; found {
		items, ok := rawItems.(map[string]interface{})
		if ok {
			cleanedItems, err := stripUnsupportedSchemaFieldsBounded(items, path+".items", fallbacks, truncated, depth+1, fields)
			if err != nil {
				return nil, err
			}
			if cleanedItems == nil {
				appendSchemaFallback(fallbacks, truncated, path, "array item schema requires YAML")
				return nil, nil
			}
			cleaned["items"] = cleanedItems
		}
	}

	if rawAdditional, found := raw["additionalProperties"]; found {
		additional, ok := rawAdditional.(map[string]interface{})
		if ok {
			cleanedAdditional, err := stripUnsupportedSchemaFieldsBounded(additional, path+".additionalProperties", fallbacks, truncated, depth+1, fields)
			if err != nil {
				return nil, err
			}
			if cleanedAdditional == nil {
				delete(cleaned, "additionalProperties")
			} else {
				cleaned["additionalProperties"] = cleanedAdditional
			}
		}
	}
	return cleaned, nil
}

// filteredRequiredSchemaNames removes fields omitted from a partial form while preserving malformed declarations for strict validation.
func filteredRequiredSchemaNames(value interface{}, properties map[string]interface{}) interface{} {
	items, ok := value.([]interface{})
	if !ok {
		return value
	}
	filtered := make([]interface{}, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return value
		}
		if _, found := properties[name]; found {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// appendSchemaFallback adds one safe field fallback until the response bound is reached.
func appendSchemaFallback(fallbacks *[]SchemaFallback, truncated *bool, path, reason string) {
	if fallbacks == nil || truncated == nil {
		return
	}
	if len(*fallbacks) >= resourceSchemaMaxFallbacks {
		*truncated = true
		return
	}
	*fallbacks = append(*fallbacks, SchemaFallback{Path: path, Reason: reason})
}

// schemaPath joins a validated schema property name to a bounded display path.
func schemaPath(parent, name string) string {
	if parent == "$" {
		return "$." + name
	}
	return parent + "." + name
}

// schemaFallbackMessage summarizes field-level fallback paths without returning unbounded schema details.
func schemaFallbackMessage(fallbacks []SchemaFallback, truncated bool) string {
	paths := make([]string, 0, minInt(len(fallbacks), 6))
	for _, fallback := range fallbacks {
		if len(paths) >= 6 {
			break
		}
		paths = append(paths, fallback.Path)
	}
	message := fmt.Sprintf("Some schema fields use the YAML fallback: %s.", strings.Join(paths, ", "))
	if truncated || len(fallbacks) > len(paths) {
		message += " Additional unsupported fields are available in YAML."
	}
	return message
}

// markStatusSchemaFieldReadOnly marks the CRD status property and all descendants as read-only.
func markStatusSchemaFieldReadOnly(field *SchemaField) {
	if field == nil {
		return
	}
	for index := range field.Properties {
		if field.Properties[index].Name == "status" {
			markSchemaFieldReadOnly(&field.Properties[index])
		}
	}
}

// markSchemaFieldReadOnly recursively locks a schema subtree without changing its shape.
func markSchemaFieldReadOnly(field *SchemaField) {
	if field == nil {
		return
	}
	field.ReadOnly = true
	for index := range field.Properties {
		markSchemaFieldReadOnly(&field.Properties[index])
	}
	if field.Items != nil {
		markSchemaFieldReadOnly(field.Items)
	}
	if field.AdditionalProperties != nil {
		markSchemaFieldReadOnly(field.AdditionalProperties)
	}
}

// markSchemaFieldReadOnlyPath locks one validated scale status path when no full status subresource exists.
func markSchemaFieldReadOnlyPath(field *SchemaField, path string) {
	segments, _, ok := parseSafeCRDFieldPath(path)
	if !ok {
		return
	}
	markSchemaFieldReadOnlySegments(field, segments)
}

// markSchemaFieldReadOnlySegments walks object properties without evaluating arbitrary schema expressions.
func markSchemaFieldReadOnlySegments(field *SchemaField, segments []string) {
	if field == nil || len(segments) == 0 {
		if field != nil {
			markSchemaFieldReadOnly(field)
		}
		return
	}
	for index := range field.Properties {
		if field.Properties[index].Name == segments[0] {
			markSchemaFieldReadOnlySegments(&field.Properties[index], segments[1:])
		}
	}
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
