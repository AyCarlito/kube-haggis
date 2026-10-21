package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/openapi"
	openapi3 "k8s.io/client-go/openapi3"
	"sigs.k8s.io/yaml"
)

const (
	// resourceSchemaSourceOpenAPIV3 identifies a schema read from the version-specific OpenAPI v3 document.
	resourceSchemaSourceOpenAPIV3 = "openapi-v3"
	// resourceSchemaSourceOpenAPIV2 identifies a schema read from the compatibility OpenAPI v2 document.
	resourceSchemaSourceOpenAPIV2 = "openapi-v2"
	// resourceSchemaSourceCRD identifies the existing CRD schema fallback.
	resourceSchemaSourceCRD = "crd"
	// resourceSchemaCacheMaxEntries bounds normalized schemas retained by one service process.
	resourceSchemaCacheMaxEntries = 128
)

// resourceSchemaDiscovery is the authenticated discovery seam needed for exact GVR and OpenAPI reads.
type resourceSchemaDiscovery interface {
	ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error)
	OpenAPIV3() openapi.Client
	OpenAPISchema() (*openapi_v2.Document, error)
}

// resourceSchemaCacheKey scopes a normalized schema to a kubeconfig revision and exact GVR.
type resourceSchemaCacheKey struct {
	contextName string
	revision    uint64
	group       string
	version     string
	resource    string
}

// schemaSourceFailure stores a safe failure classification while trying schema sources.
type schemaSourceFailure struct {
	state   string
	message string
}

// normalizedOpenAPISchema contains the bounded form model and field-level YAML fallbacks from one source.
type normalizedOpenAPISchema struct {
	field              *SchemaField
	fallbacks          []SchemaFallback
	fallbacksTruncated bool
}

// schemaReferenceResolver holds local OpenAPI definitions and bounded recursion state.
type schemaReferenceResolver struct {
	definitions map[string]map[string]interface{}
	prefix      string
	stack       map[string]bool
}

// loadResourceSchema loads OpenAPI schema data when discovery is supplied and preserves the direct CRD test seam otherwise.
func loadResourceSchema(ctx context.Context, client dynamic.Interface, group, version, resource string, discoveries ...resourceSchemaDiscovery) (ResourceSchema, error) {
	var (
		result ResourceSchema
		err    error
	)
	if len(discoveries) > 0 && discoveries[0] != nil {
		result, err = loadOpenAPIResourceSchema(ctx, client, discoveries[0], group, version, resource)
	} else {
		result, err = loadCRDResourceSchema(ctx, client, group, version, resource)
		if err == nil && result.Schema != nil {
			result.Source = resourceSchemaSourceCRD
		}
	}
	lockResourceSchemaFields(result.Schema)
	return result, err
}

// loadOpenAPIResourceSchema resolves one discovered GVR and prefers its authenticated version-specific OpenAPI schema.
func loadOpenAPIResourceSchema(ctx context.Context, dynamicClient dynamic.Interface, discoveryClient resourceSchemaDiscovery, group, version, resource string) (ResourceSchema, error) {
	result := ResourceSchema{Group: group, Version: version, Resource: resource}
	kind, failure := discoveredSchemaKind(discoveryClient, group, version, resource)
	if failure != nil {
		result.State = failure.state
		result.Message = failure.message
		return result, nil
	}
	result.Kind = kind

	v3Failure := schemaSourceFailure{state: resourceSchemaStateMissing, message: "This Kubernetes API server did not publish a matching OpenAPI v3 resource schema; checking the OpenAPI v2 compatibility document."}
	v3Document, v3Err := loadOpenAPIV3Document(discoveryClient, schema.GroupVersion{Group: group, Version: version})
	if v3Err == nil {
		definitions, definitionsErr := openAPIV3Definitions(v3Document)
		if definitionsErr != nil {
			v3Failure = schemaSourceFailure{state: resourceSchemaStateMalformed, message: "The Kubernetes API server returned a malformed OpenAPI v3 document; checking the OpenAPI v2 compatibility document."}
		} else {
			raw, found, selectionErr := selectOpenAPISchema(definitions, group, version, kind)
			if selectionErr != nil {
				v3Failure = schemaSourceFailure{state: resourceSchemaStateMalformed, message: "The Kubernetes API server returned a malformed OpenAPI v3 resource schema; use the YAML editor."}
			} else if found {
				normalized, normalizeErr := normalizeOpenAPISchema(raw, definitions, "#/components/schemas/")
				if normalizeErr != nil {
					result.State, result.Message = schemaNormalizationFailure(normalizeErr)
					return result, nil
				}
				return completeOpenAPIResourceSchema(result, normalized, resourceSchemaSourceOpenAPIV3), nil
			}
		}
	} else {
		v3Failure = classifySchemaSourceFailure(v3Err, "OpenAPI v3 is unavailable")
	}

	v2Failure := schemaSourceFailure{state: resourceSchemaStateMissing, message: "This Kubernetes API server did not publish a matching OpenAPI v2 compatibility schema; use the YAML editor."}
	v2Document, v2Err := discoveryClient.OpenAPISchema()
	if v2Err == nil {
		if v2Document != nil {
			definitions, definitionsErr := openAPIV2Definitions(v2Document)
			if definitionsErr != nil {
				v2Failure = schemaSourceFailure{state: resourceSchemaStateMalformed, message: "The Kubernetes API server returned a malformed OpenAPI v2 compatibility document; use the YAML editor."}
			} else {
				raw, found, selectionErr := selectOpenAPISchema(definitions, group, version, kind)
				if selectionErr != nil {
					v2Failure = schemaSourceFailure{state: resourceSchemaStateMalformed, message: "The Kubernetes API server returned a malformed OpenAPI v2 resource schema; use the YAML editor."}
				} else if found {
					normalized, normalizeErr := normalizeOpenAPISchema(raw, definitions, "#/definitions/")
					if normalizeErr != nil {
						result.State, result.Message = schemaNormalizationFailure(normalizeErr)
						return result, nil
					}
					return completeOpenAPIResourceSchema(result, normalized, resourceSchemaSourceOpenAPIV2), nil
				}
			}
		}
	} else {
		v2Failure = classifySchemaSourceFailure(v2Err, "OpenAPI v2 is unavailable")
	}

	if dynamicClient != nil && group != "" && group != "apiextensions.k8s.io" {
		crdResult, crdErr := loadCRDResourceSchema(ctx, dynamicClient, group, version, resource)
		if crdErr == nil && crdResult.State == resourceSchemaStateAvailable && crdResult.Schema != nil {
			crdResult.Source = resourceSchemaSourceCRD
			return crdResult, nil
		}
	}
	result.State, result.Message = finalSchemaSourceFailure(v3Failure, v2Failure)
	return result, nil
}

// discoveredSchemaKind reads the exact resource kind from the requested group-version discovery response.
func discoveredSchemaKind(discoveryClient resourceSchemaDiscovery, group, version, resource string) (string, *schemaSourceFailure) {
	resources, err := discoveryClient.ServerResourcesForGroupVersion(schema.GroupVersion{Group: group, Version: version}.String())
	if err != nil {
		failure := classifySchemaSourceFailure(err, "The requested Kubernetes API is unavailable")
		return "", &failure
	}
	if resources == nil {
		failure := schemaSourceFailure{state: resourceSchemaStateMalformed, message: "Kubernetes returned no resource discovery for the requested API version; use the YAML editor."}
		return "", &failure
	}
	for _, item := range resources.APIResources {
		if item.Name != resource || strings.Contains(item.Name, "/") {
			continue
		}
		if strings.TrimSpace(item.Kind) == "" {
			failure := schemaSourceFailure{state: resourceSchemaStateMalformed, message: "Kubernetes did not declare a kind for the requested resource; use the YAML editor."}
			return "", &failure
		}
		return item.Kind, nil
	}
	failure := schemaSourceFailure{state: resourceSchemaStateMissing, message: "The requested group, version, and resource are not present in Kubernetes discovery; use the YAML editor."}
	return "", &failure
}

// loadOpenAPIV3Document retrieves one exact group-version OpenAPI v3 document through client-go.
func loadOpenAPIV3Document(discoveryClient resourceSchemaDiscovery, groupVersion schema.GroupVersion) (map[string]interface{}, error) {
	client := discoveryClient.OpenAPIV3()
	if client == nil {
		return nil, fmt.Errorf("OpenAPI v3 client is unavailable")
	}
	return openapi3.NewRoot(client).GVSpecAsMap(groupVersion)
}

// openAPIV3Definitions extracts reusable schemas from one version-specific OpenAPI v3 document.
func openAPIV3Definitions(document map[string]interface{}) (map[string]map[string]interface{}, error) {
	components, ok := document["components"].(map[string]interface{})
	if !ok || components == nil {
		return nil, fmt.Errorf("OpenAPI v3 components are missing")
	}
	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok || schemas == nil {
		return nil, fmt.Errorf("OpenAPI v3 component schemas are missing")
	}
	return openAPIRawDefinitions(schemas)
}

// openAPIV2Definitions converts the typed client-go OpenAPI v2 document into the shared raw schema representation.
func openAPIV2Definitions(document *openapi_v2.Document) (map[string]map[string]interface{}, error) {
	if document == nil || document.Definitions == nil || len(document.Definitions.AdditionalProperties) == 0 {
		return nil, fmt.Errorf("OpenAPI v2 definitions are missing")
	}
	definitions := make(map[string]map[string]interface{}, len(document.Definitions.AdditionalProperties))
	for _, definition := range document.Definitions.AdditionalProperties {
		if definition == nil || strings.TrimSpace(definition.Name) == "" || definition.Value == nil {
			return nil, fmt.Errorf("OpenAPI v2 contains a malformed definition")
		}
		raw, err := openAPIV2SchemaToRaw(definition.Value)
		if err != nil {
			return nil, err
		}
		definitions[definition.Name] = raw
	}
	return definitions, nil
}

// openAPIRawDefinitions validates and copies raw component schemas without exposing source document objects.
func openAPIRawDefinitions(values map[string]interface{}) (map[string]map[string]interface{}, error) {
	definitions := make(map[string]map[string]interface{}, len(values))
	for name, value := range values {
		raw, ok := value.(map[string]interface{})
		if !ok || raw == nil {
			return nil, fmt.Errorf("OpenAPI definition %q is malformed", name)
		}
		definitions[name] = copySchemaMap(raw)
	}
	return definitions, nil
}

// selectOpenAPISchema selects exactly one schema by its GVK extension or Kubernetes model name.
func selectOpenAPISchema(definitions map[string]map[string]interface{}, group, version, kind string) (map[string]interface{}, bool, error) {
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	extensionMatches := make([]string, 0, 1)
	for _, name := range names {
		if schemaMatchesGVK(definitions[name], group, version, kind) {
			extensionMatches = append(extensionMatches, name)
		}
	}
	if len(extensionMatches) > 1 {
		return nil, false, fmt.Errorf("OpenAPI contains multiple schemas for the requested GVK")
	}
	if len(extensionMatches) == 1 {
		return definitions[extensionMatches[0]], true, nil
	}
	for _, candidate := range openAPISchemaNameCandidates(group, version, kind) {
		if raw, found := definitions[candidate]; found {
			if schemaHasGVKExtension(raw) && !schemaMatchesGVK(raw, group, version, kind) {
				continue
			}
			return raw, true, nil
		}
	}
	return nil, false, nil
}

// openAPISchemaNameCandidates returns Kubernetes' generated model names for one exact GVK.
func openAPISchemaNameCandidates(group, version, kind string) []string {
	if group == "" {
		return []string{fmt.Sprintf("io.k8s.api.core.%s.%s", version, kind)}
	}
	packageGroup := strings.TrimSuffix(group, ".k8s.io")
	reversedGroup := reverseSchemaGroup(group)
	candidates := []string{
		fmt.Sprintf("io.k8s.api.%s.%s.%s", packageGroup, version, kind),
		fmt.Sprintf("io.k8s.api.%s.%s.%s", group, version, kind),
		fmt.Sprintf("io.k8s.%s.%s.%s", packageGroup, version, kind),
		fmt.Sprintf("io.k8s.%s.%s.%s", group, version, kind),
		fmt.Sprintf("%s.%s.%s", reversedGroup, version, kind),
		fmt.Sprintf("%s.%s.%s", group, version, kind),
	}
	return candidates
}

// reverseSchemaGroup returns the model-name group encoding used by the CRD OpenAPI builder.
func reverseSchemaGroup(group string) string {
	parts := strings.Split(group, ".")
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, ".")
}

// schemaMatchesGVK verifies the exact Kubernetes GVK extension on one OpenAPI schema.
func schemaMatchesGVK(raw map[string]interface{}, group, version, kind string) bool {
	value, found := raw["x-kubernetes-group-version-kind"]
	if !found || value == nil {
		return false
	}
	match := func(candidate map[string]interface{}) bool {
		candidateGroup, groupOK := candidate["group"].(string)
		candidateVersion, versionOK := candidate["version"].(string)
		candidateKind, kindOK := candidate["kind"].(string)
		return groupOK && versionOK && kindOK && candidateGroup == group && candidateVersion == version && candidateKind == kind
	}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if candidate, ok := item.(map[string]interface{}); ok && match(candidate) {
				return true
			}
		}
	case map[string]interface{}:
		return match(typed)
	}
	return false
}

// schemaHasGVKExtension reports whether a schema declares any Kubernetes GVK extension.
func schemaHasGVKExtension(raw map[string]interface{}) bool {
	value, found := raw["x-kubernetes-group-version-kind"]
	if !found || value == nil {
		return false
	}
	switch typed := value.(type) {
	case []interface{}:
		return len(typed) > 0
	case map[string]interface{}:
		return len(typed) > 0
	default:
		return true
	}
}

// normalizeOpenAPISchema resolves bounded local references and converts one raw OpenAPI schema to the form model.
func normalizeOpenAPISchema(raw map[string]interface{}, definitions map[string]map[string]interface{}, prefix string) (normalizedOpenAPISchema, error) {
	resolver := &schemaReferenceResolver{definitions: definitions, prefix: prefix, stack: make(map[string]bool)}
	fallbacks := make([]SchemaFallback, 0)
	fallbacksTruncated := false
	resolveFields := 0
	resolved, err := resolver.resolve(raw, "$", 0, &resolveFields, &fallbacks, &fallbacksTruncated)
	if err != nil {
		return normalizedOpenAPISchema{}, err
	}
	field, err := parseSchemaFieldWithFallbacks(resolved, "", false, 0, new(int), &fallbacks, &fallbacksTruncated)
	if err != nil {
		return normalizedOpenAPISchema{}, err
	}
	if field == nil {
		return normalizedOpenAPISchema{}, fmt.Errorf("root schema is not representable")
	}
	return normalizedOpenAPISchema{field: field, fallbacks: fallbacks, fallbacksTruncated: fallbacksTruncated}, nil
}

// resolve resolves local references and recursively bounds nested reference traversal before form parsing.
func (r *schemaReferenceResolver) resolve(raw map[string]interface{}, path string, depth int, fields *int, fallbacks *[]SchemaFallback, truncated *bool) (map[string]interface{}, error) {
	if raw == nil {
		return nil, fmt.Errorf("schema field at %s is empty", path)
	}
	if depth > resourceSchemaMaxDepth {
		if path == "$" {
			return nil, fmt.Errorf("schema reference nesting exceeds %d levels", resourceSchemaMaxDepth)
		}
		appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("schema reference nesting exceeds %d levels", resourceSchemaMaxDepth))
		return nil, nil
	}
	if fields == nil {
		return nil, fmt.Errorf("schema reference field counter is unavailable")
	}
	*fields++
	if *fields > resourceSchemaMaxFields {
		if path == "$" {
			return nil, fmt.Errorf("schema contains more than %d referenced fields", resourceSchemaMaxFields)
		}
		appendSchemaFallback(fallbacks, truncated, path, fmt.Sprintf("schema contains more than %d referenced fields", resourceSchemaMaxFields))
		return nil, nil
	}
	if unwrapped, ok := r.singleReferenceAllOf(raw); ok {
		return r.resolve(unwrapped, path, depth, fields, fallbacks, truncated)
	}

	if rawReference, found := raw["$ref"]; found {
		ref, ok := rawReference.(string)
		if !ok || !strings.HasPrefix(ref, r.prefix) {
			if path == "$" {
				return nil, fmt.Errorf("schema reference %q is not a supported local reference", fmt.Sprint(rawReference))
			}
			appendSchemaFallback(fallbacks, truncated, path, "unsupported external or malformed schema reference")
			return nil, nil
		}
		target, found := r.definitionForReference(ref)
		if !found {
			if path == "$" {
				return nil, fmt.Errorf("schema reference %q is missing", ref)
			}
			appendSchemaFallback(fallbacks, truncated, path, "schema reference is missing")
			return nil, nil
		}
		if r.stack[ref] {
			if path == "$" {
				return nil, fmt.Errorf("schema reference %q is recursive", ref)
			}
			appendSchemaFallback(fallbacks, truncated, path, "recursive schema reference requires YAML")
			return nil, nil
		}
		r.stack[ref] = true
		defer delete(r.stack, ref)
		merged := copySchemaMap(target)
		for key, value := range raw {
			if key != "$ref" {
				if key == "required" {
					value = mergeRequiredSchemaValues(merged[key], value)
				}
				merged[key] = value
			}
		}
		return r.resolve(merged, path, depth+1, fields, fallbacks, truncated)
	}

	cleaned := copySchemaMap(raw)
	if rawProperties, found := raw["properties"]; found {
		properties, ok := rawProperties.(map[string]interface{})
		if !ok {
			if path == "$" {
				return nil, fmt.Errorf("schema properties at %s are malformed", path)
			}
			appendSchemaFallback(fallbacks, truncated, path, "malformed properties require YAML")
			return nil, nil
		}
		resolvedProperties := make(map[string]interface{}, len(properties))
		names := orderedSchemaPropertyNames(properties, path == "$")
		for _, name := range names {
			property, ok := properties[name].(map[string]interface{})
			if !ok {
				appendSchemaFallback(fallbacks, truncated, schemaPath(path, name), "malformed field schema requires YAML")
				continue
			}
			child, childErr := r.resolve(property, schemaPath(path, name), depth+1, fields, fallbacks, truncated)
			if childErr != nil {
				return nil, childErr
			}
			if child != nil {
				resolvedProperties[name] = child
			}
		}
		cleaned["properties"] = resolvedProperties
		cleaned["required"] = filteredRequiredSchemaNames(raw["required"], resolvedProperties)
	}
	if rawItems, found := raw["items"]; found {
		items, ok := rawItems.(map[string]interface{})
		if !ok {
			if path == "$" {
				return nil, fmt.Errorf("schema array items at %s are malformed", path)
			}
			appendSchemaFallback(fallbacks, truncated, path, "malformed array item schema requires YAML")
			return nil, nil
		}
		resolvedItems, itemsErr := r.resolve(items, path+".items", depth+1, fields, fallbacks, truncated)
		if itemsErr != nil {
			return nil, itemsErr
		}
		if resolvedItems == nil {
			if path == "$" {
				return nil, fmt.Errorf("root array item schema requires YAML")
			}
			appendSchemaFallback(fallbacks, truncated, path, "array item schema requires YAML")
			return nil, nil
		}
		cleaned["items"] = resolvedItems
	}
	if rawAdditional, found := raw["additionalProperties"]; found {
		additional, ok := rawAdditional.(map[string]interface{})
		if ok {
			resolvedAdditional, additionalErr := r.resolve(additional, path+".additionalProperties", depth+1, fields, fallbacks, truncated)
			if additionalErr != nil {
				return nil, additionalErr
			}
			if resolvedAdditional == nil {
				delete(cleaned, "additionalProperties")
			} else {
				cleaned["additionalProperties"] = resolvedAdditional
			}
		}
	}
	return cleaned, nil
}

// definitionForReference resolves either a fully prefixed local reference or its extracted bare definition name.
func (r *schemaReferenceResolver) definitionForReference(ref string) (map[string]interface{}, bool) {
	if target, found := r.definitions[ref]; found {
		return target, true
	}
	bare := strings.TrimPrefix(ref, r.prefix)
	if bare == ref {
		return nil, false
	}
	target, found := r.definitions[bare]
	return target, found
}

// singleReferenceAllOf unwraps only one local reference with supported metadata and validation siblings.
func (r *schemaReferenceResolver) singleReferenceAllOf(raw map[string]interface{}) (map[string]interface{}, bool) {
	value, found := raw["allOf"]
	if !found || value == nil {
		return nil, false
	}
	branches, ok := value.([]interface{})
	if !ok || len(branches) != 1 {
		return nil, false
	}
	branch, ok := branches[0].(map[string]interface{})
	if !ok || branch == nil {
		return nil, false
	}
	ref, ok := branch["$ref"].(string)
	if !ok || !strings.HasPrefix(ref, r.prefix) {
		return nil, false
	}
	merged := map[string]interface{}{"$ref": ref}
	for _, siblingSource := range []map[string]interface{}{branch, raw} {
		for key, sibling := range siblingSource {
			switch key {
			case "$ref", "allOf":
				continue
			case "title", "description", "format", "readOnly", "default", "enum", "minimum", "exclusiveMinimum", "maximum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "minItems", "maxItems", "uniqueItems", "required":
			case "x-kubernetes-patch-strategy", "x-kubernetes-patch-merge-key", "x-kubernetes-list-type", "x-kubernetes-list-map-keys", "x-kubernetes-map-type":
			default:
				return nil, false
			}
			if key == "required" {
				sibling = mergeRequiredSchemaValues(merged[key], sibling)
			}
			merged[key] = sibling
		}
	}
	return merged, true
}

// mergeRequiredSchemaValues preserves both required lists when a reference wrapper adds required fields.
func mergeRequiredSchemaValues(existing, overlay interface{}) interface{} {
	if existing == nil {
		return overlay
	}
	existingNames, existingOK := existing.([]interface{})
	overlayNames, overlayOK := overlay.([]interface{})
	if !existingOK {
		return existing
	}
	if !overlayOK {
		return overlay
	}
	merged := append([]interface{}{}, existingNames...)
	seen := make(map[string]struct{}, len(merged))
	for _, item := range merged {
		if name, ok := item.(string); ok {
			seen[name] = struct{}{}
		}
	}
	for _, item := range overlayNames {
		name, ok := item.(string)
		if !ok {
			return overlay
		}
		if _, found := seen[name]; found {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	return merged
}

// completeOpenAPIResourceSchema builds the public result from a normalized OpenAPI source.
func completeOpenAPIResourceSchema(result ResourceSchema, normalized normalizedOpenAPISchema, source string) ResourceSchema {
	result.State = resourceSchemaStateAvailable
	result.Source = source
	result.Schema = normalized.field
	result.Fallbacks = normalized.fallbacks
	result.FallbacksTruncated = normalized.fallbacksTruncated
	if len(normalized.fallbacks) > 0 {
		result.Message = schemaFallbackMessage(normalized.fallbacks, normalized.fallbacksTruncated)
	}
	lockResourceSchemaFields(result.Schema)
	return result
}

// schemaNormalizationFailure maps parser errors to explicit malformed or unsupported browser states.
func schemaNormalizationFailure(err error) (string, string) {
	message := fmt.Sprintf("The OpenAPI schema cannot be represented safely as a form: %v; use the YAML editor.", err)
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unsupported") || strings.Contains(text, "recursive") || strings.Contains(text, "nesting exceeds") || strings.Contains(text, "more than") {
		return resourceSchemaStateUnsupported, message
	}
	return resourceSchemaStateMalformed, message
}

// classifySchemaSourceFailure turns backend OpenAPI errors into safe explicit states.
func classifySchemaSourceFailure(err error, unavailableMessage string) schemaSourceFailure {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return schemaSourceFailure{state: resourceSchemaStatePermissionDenied, message: "Kubernetes RBAC denied access to the OpenAPI schema; use the YAML editor."}
	}
	var groupVersionNotFound *openapi3.GroupVersionNotFoundError
	if apierrors.IsNotFound(err) || errors.As(err, &groupVersionNotFound) {
		return schemaSourceFailure{state: resourceSchemaStateMissing, message: "The Kubernetes API server does not expose a matching OpenAPI schema for this resource; use the YAML editor."}
	}
	return schemaSourceFailure{state: resourceSchemaStateUnavailable, message: unavailableMessage + "; use the YAML editor."}
}

// finalSchemaSourceFailure selects the safest explicit state after both OpenAPI documents fail.
func finalSchemaSourceFailure(v3Failure, v2Failure schemaSourceFailure) (string, string) {
	if v3Failure.state == resourceSchemaStatePermissionDenied || v2Failure.state == resourceSchemaStatePermissionDenied {
		return resourceSchemaStatePermissionDenied, "Kubernetes RBAC denied access to the OpenAPI schema; use the YAML editor."
	}
	if v3Failure.state == resourceSchemaStateUnavailable || v2Failure.state == resourceSchemaStateUnavailable {
		return resourceSchemaStateUnavailable, "The Kubernetes OpenAPI schema API is unavailable; use the YAML editor."
	}
	if v3Failure.state == resourceSchemaStateMalformed || v2Failure.state == resourceSchemaStateMalformed {
		return resourceSchemaStateMalformed, "The Kubernetes OpenAPI schema document is malformed; use the YAML editor."
	}
	return resourceSchemaStateMissing, "This Kubernetes API server does not publish a usable OpenAPI schema for the resource; use the YAML editor."
}

// openAPIV2SchemaToRaw converts a client-go OpenAPI v2 schema while retaining supported metadata and validation.
func openAPIV2SchemaToRaw(source *openapi_v2.Schema) (map[string]interface{}, error) {
	if source == nil {
		return nil, fmt.Errorf("OpenAPI v2 schema is empty")
	}
	raw := make(map[string]interface{})
	if source.XRef != "" {
		raw["$ref"] = source.XRef
	}
	if typeItem := source.Type; typeItem != nil {
		types := typeItem.Value
		if len(types) == 1 {
			raw["type"] = types[0]
		} else if len(types) > 1 {
			raw["type"] = types[0]
			oneOf := make([]interface{}, 0, len(types))
			for _, typeName := range types {
				oneOf = append(oneOf, map[string]interface{}{"type": typeName})
			}
			raw["oneOf"] = oneOf
		}
	}
	if source.Title != "" {
		raw["title"] = source.Title
	}
	if source.Description != "" {
		raw["description"] = source.Description
	}
	if source.Format != "" {
		raw["format"] = source.Format
	}
	if source.ReadOnly {
		raw["readOnly"] = true
	}
	if source.Default != nil {
		value, ok := openAPIV2AnyValue(source.Default)
		if !ok {
			return nil, fmt.Errorf("OpenAPI v2 default value is malformed")
		}
		raw["default"] = value
	}
	if source.Maximum != 0 || source.ExclusiveMaximum {
		raw["maximum"] = source.Maximum
	}
	if source.ExclusiveMaximum {
		raw["exclusiveMaximum"] = true
	}
	if source.Minimum != 0 || source.ExclusiveMinimum {
		raw["minimum"] = source.Minimum
	}
	if source.ExclusiveMinimum {
		raw["exclusiveMinimum"] = true
	}
	if source.MultipleOf != 0 {
		raw["multipleOf"] = source.MultipleOf
	}
	if source.MaxLength != 0 {
		raw["maxLength"] = source.MaxLength
	}
	if source.MinLength != 0 {
		raw["minLength"] = source.MinLength
	}
	if source.Pattern != "" {
		raw["pattern"] = source.Pattern
	}
	if source.MaxItems != 0 {
		raw["maxItems"] = source.MaxItems
	}
	if source.MinItems != 0 {
		raw["minItems"] = source.MinItems
	}
	if source.UniqueItems {
		raw["uniqueItems"] = true
	}
	if len(source.Required) > 0 {
		raw["required"] = stringSliceAsInterfaces(source.Required)
	}
	if len(source.Enum) > 0 {
		enum := make([]interface{}, 0, len(source.Enum))
		for _, item := range source.Enum {
			value, ok := openAPIV2AnyValue(item)
			if !ok {
				return nil, fmt.Errorf("OpenAPI v2 enum value is malformed")
			}
			enum = append(enum, value)
		}
		raw["enum"] = enum
	}
	if len(source.AllOf) > 0 {
		allOf := make([]interface{}, 0, len(source.AllOf))
		for _, item := range source.AllOf {
			converted, err := openAPIV2SchemaToRaw(item)
			if err != nil {
				return nil, err
			}
			allOf = append(allOf, converted)
		}
		raw["allOf"] = allOf
	}
	if source.Properties != nil {
		properties := make(map[string]interface{}, len(source.Properties.AdditionalProperties))
		for _, property := range source.Properties.AdditionalProperties {
			if property == nil || property.Name == "" || property.Value == nil {
				return nil, fmt.Errorf("OpenAPI v2 property is malformed")
			}
			converted, err := openAPIV2SchemaToRaw(property.Value)
			if err != nil {
				return nil, err
			}
			properties[property.Name] = converted
		}
		raw["properties"] = properties
	}
	if source.Items != nil {
		if len(source.Items.Schema) != 1 {
			raw["oneOf"] = []interface{}{"array item schema is not singular"}
		} else {
			converted, err := openAPIV2SchemaToRaw(source.Items.Schema[0])
			if err != nil {
				return nil, err
			}
			raw["items"] = converted
		}
	}
	if source.AdditionalProperties != nil {
		switch typed := source.AdditionalProperties.Oneof.(type) {
		case *openapi_v2.AdditionalPropertiesItem_Schema:
			converted, err := openAPIV2SchemaToRaw(typed.Schema)
			if err != nil {
				return nil, err
			}
			raw["additionalProperties"] = converted
		case *openapi_v2.AdditionalPropertiesItem_Boolean:
			raw["additionalProperties"] = typed.Boolean
		case nil:
			return nil, fmt.Errorf("OpenAPI v2 additional properties are malformed")
		default:
			return nil, fmt.Errorf("OpenAPI v2 additional properties are unsupported")
		}
	}
	for _, extension := range source.VendorExtension {
		if extension == nil || extension.Name == "" || extension.Value == nil {
			continue
		}
		value, ok := openAPIV2AnyValue(extension.Value)
		if ok {
			raw[extension.Name] = value
		}
	}
	return raw, nil
}

// openAPIV2AnyValue converts one gnostic OpenAPI v2 Any value into JSON-compatible data.
func openAPIV2AnyValue(value *openapi_v2.Any) (interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if strings.TrimSpace(value.Yaml) != "" {
		var decoded interface{}
		if err := yaml.Unmarshal([]byte(value.Yaml), &decoded); err != nil {
			return nil, false
		}
		return normalizeSchemaJSON(decoded)
	}
	if value.Value != nil && len(value.Value.Value) > 0 {
		var decoded interface{}
		if err := json.Unmarshal(value.Value.Value, &decoded); err != nil {
			return nil, false
		}
		return normalizeSchemaJSON(decoded)
	}
	return nil, false
}

// stringSliceAsInterfaces converts v2 required names to the raw JSON representation used by the CRD parser.
func stringSliceAsInterfaces(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

// copySchemaMap creates a shallow copy before a reference merge or traversal rewrite.
func copySchemaMap(raw map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		copy[key] = value
	}
	return copy
}

// lockResourceSchemaFields marks Kubernetes identity, server-managed metadata, and status descendants read-only.
func lockResourceSchemaFields(field *SchemaField) {
	if field == nil {
		return
	}
	for index := range field.Properties {
		property := &field.Properties[index]
		switch property.Name {
		case "apiVersion", "kind", "status":
			markSchemaFieldReadOnly(property)
		case "metadata":
			lockMetadataSchemaField(property)
		}
	}
}

// lockMetadataSchemaField marks identity and server-owned ObjectMeta fields without disabling safe labels or annotations.
func lockMetadataSchemaField(field *SchemaField) {
	if field == nil {
		return
	}
	for index := range field.Properties {
		property := &field.Properties[index]
		switch property.Name {
		case "name", "namespace", "uid", "resourceVersion", "managedFields", "creationTimestamp", "deletionTimestamp", "deletionGracePeriodSeconds", "generation", "selfLink":
			property.ReadOnly = true
		}
	}
}

// cachedResourceSchema returns a detached normalized schema for one revision-scoped GVR.
func (s *Service) cachedResourceSchema(key resourceSchemaCacheKey) (ResourceSchema, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resourceSchemaCache == nil {
		return ResourceSchema{}, false
	}
	result, found := s.resourceSchemaCache[key]
	if !found {
		return ResourceSchema{}, false
	}
	return cloneResourceSchema(result), true
}

// storeResourceSchema retains one detached normalized schema while enforcing the process-wide cache bound.
func (s *Service) storeResourceSchema(key resourceSchemaCacheKey, result ResourceSchema) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision != key.revision {
		return
	}
	if s.resourceSchemaCache == nil {
		s.resourceSchemaCache = make(map[resourceSchemaCacheKey]ResourceSchema)
	}
	if _, found := s.resourceSchemaCache[key]; found {
		s.resourceSchemaCache[key] = cloneResourceSchema(result)
		return
	}
	if len(s.resourceSchemaCacheOrder) >= resourceSchemaCacheMaxEntries {
		oldest := s.resourceSchemaCacheOrder[0]
		s.resourceSchemaCacheOrder = s.resourceSchemaCacheOrder[1:]
		delete(s.resourceSchemaCache, oldest)
	}
	s.resourceSchemaCacheOrder = append(s.resourceSchemaCacheOrder, key)
	s.resourceSchemaCache[key] = cloneResourceSchema(result)
}

// cloneResourceSchema returns a detached copy safe to return from the schema cache.
func cloneResourceSchema(source ResourceSchema) ResourceSchema {
	result := source
	result.Fallbacks = append([]SchemaFallback(nil), source.Fallbacks...)
	result.Schema = cloneSchemaField(source.Schema)
	return result
}

// cloneSchemaField recursively copies one normalized schema field and all JSON-compatible metadata.
func cloneSchemaField(source *SchemaField) *SchemaField {
	if source == nil {
		return nil
	}
	result := *source
	result.ScalarTypes = append([]string(nil), source.ScalarTypes...)
	result.Enum = append([]interface{}(nil), source.Enum...)
	if source.Default != nil {
		result.Default, _ = normalizeSchemaJSON(source.Default)
	}
	result.Properties = make([]SchemaField, len(source.Properties))
	for index := range source.Properties {
		result.Properties[index] = *cloneSchemaField(&source.Properties[index])
	}
	result.Items = cloneSchemaField(source.Items)
	result.AdditionalProperties = cloneSchemaField(source.AdditionalProperties)
	return &result
}
