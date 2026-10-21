package kube

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	maxCRDPrinterColumns        = 16
	maxCRDPrinterColumnWarnings = 16
	maxCRDPrinterColumnName     = 64
	maxCRDPrinterColumnPath     = 256
	maxCRDPrinterColumnSegments = 16
	maxCRDPrinterColumnValue    = 256
)

// CustomResourceCapabilities contains bounded CRD metadata attached to one served GVR.
type CustomResourceCapabilities struct {
	Group                   string                        `json:"group"`
	Version                 string                        `json:"version"`
	Kind                    string                        `json:"kind"`
	Plural                  string                        `json:"plural"`
	Resource                string                        `json:"resource"`
	Scope                   string                        `json:"scope"`
	ServedVersions          []string                      `json:"servedVersions"`
	PreferredVersion        string                        `json:"preferredVersion"`
	StorageVersion          string                        `json:"storageVersion"`
	StatusSubresource       bool                          `json:"statusSubresource"`
	ScaleSubresource        bool                          `json:"scaleSubresource"`
	ScaleSpecReplicasPath   string                        `json:"scaleSpecReplicasPath,omitempty"`
	ScaleStatusReplicasPath string                        `json:"scaleStatusReplicasPath,omitempty"`
	ScaleLabelSelectorPath  string                        `json:"scaleLabelSelectorPath,omitempty"`
	PrinterColumns          []CustomResourcePrinterColumn `json:"printerColumns,omitempty"`
	PrinterColumnWarnings   []string                      `json:"printerColumnWarnings,omitempty"`
}

// CustomResourcePrinterColumn is a safe CRD printer column with tokenized field access.
type CustomResourcePrinterColumn struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Priority    int32    `json:"priority,omitempty"`
	Path        []string `json:"path"`
}

// preferredDiscoveryVersions returns each API group's preferred discovery version.
func preferredDiscoveryVersions(groups []*metav1.APIGroup) map[string]string {
	preferred := make(map[string]string, len(groups))
	for _, group := range groups {
		if group == nil || strings.TrimSpace(group.Name) == "" {
			continue
		}
		version := strings.TrimSpace(group.PreferredVersion.Version)
		if version != "" {
			preferred[group.Name] = version
		}
	}
	return preferred
}

// customResourceCapabilitiesForCRD normalizes safe metadata for every served CRD version.
func customResourceCapabilitiesForCRD(crd *unstructured.Unstructured, preferredVersions map[string]string) (map[string]*CustomResourceCapabilities, bool) {
	if crd == nil {
		return nil, false
	}
	spec, foundSpec, specErr := unstructured.NestedMap(crd.Object, "spec")
	if specErr != nil || !foundSpec {
		return nil, false
	}
	group, foundGroup, groupErr := unstructured.NestedString(spec, "group")
	plural, foundPlural, pluralErr := unstructured.NestedString(spec, "names", "plural")
	kind, _, _ := unstructured.NestedString(spec, "names", "kind")
	scope, _, _ := unstructured.NestedString(spec, "scope")
	versions, foundVersions, versionsErr := unstructured.NestedSlice(spec, "versions")
	if groupErr != nil || pluralErr != nil || !foundGroup || !foundPlural || versionsErr != nil || !foundVersions {
		return nil, false
	}

	servedVersions := make([]string, 0, len(versions))
	storageVersion := ""
	versionObjects := make(map[string]map[string]interface{}, len(versions))
	for _, rawVersion := range versions {
		versionObject, ok := rawVersion.(map[string]interface{})
		if !ok {
			continue
		}
		versionName, foundName, nameErr := unstructured.NestedString(versionObject, "name")
		served, foundServed, servedErr := unstructured.NestedBool(versionObject, "served")
		if nameErr != nil || servedErr != nil || !foundName || !foundServed || !served || !safeCRDCoordinate(versionName) {
			continue
		}
		servedVersions = append(servedVersions, versionName)
		versionObjects[versionName] = versionObject
		storage, _, _ := unstructured.NestedBool(versionObject, "storage")
		if storage && storageVersion == "" {
			storageVersion = versionName
		}
	}
	if len(servedVersions) == 0 {
		return nil, false
	}
	if storageVersion == "" {
		storageVersion = storedCRDVersion(crd)
	}
	preferredVersion := strings.TrimSpace(preferredVersions[group])
	if !containsString(servedVersions, preferredVersion) {
		preferredVersion = servedVersions[0]
	}
	statusSubresource, scaleSubresource, scalePaths := crdSubresourceMetadata(spec)
	capabilities := make(map[string]*CustomResourceCapabilities, len(servedVersions))
	for _, version := range servedVersions {
		versionObject := versionObjects[version]
		columns, warnings := parseCRDPrinterColumns(versionObject, spec)
		capability := &CustomResourceCapabilities{
			Group:                   group,
			Version:                 version,
			Kind:                    kind,
			Plural:                  plural,
			Resource:                plural,
			Scope:                   scope,
			ServedVersions:          append([]string(nil), servedVersions...),
			PreferredVersion:        preferredVersion,
			StorageVersion:          storageVersion,
			StatusSubresource:       statusSubresource,
			ScaleSubresource:        scaleSubresource,
			ScaleSpecReplicasPath:   scalePaths.specReplicas,
			ScaleStatusReplicasPath: scalePaths.statusReplicas,
			ScaleLabelSelectorPath:  scalePaths.labelSelector,
			PrinterColumns:          columns,
			PrinterColumnWarnings:   warnings,
		}
		capabilities[version] = capability
	}
	return capabilities, true
}

// storedCRDVersion returns the first bounded stored version when spec storage metadata is incomplete.
func storedCRDVersion(crd *unstructured.Unstructured) string {
	stored, found, err := unstructured.NestedSlice(crd.Object, "status", "storedVersions")
	if err != nil || !found {
		return ""
	}
	for _, rawVersion := range stored {
		version, ok := rawVersion.(string)
		if ok && safeCRDCoordinate(version) {
			return version
		}
	}
	return ""
}

// crdScalePaths contains only validated scale subresource paths.
type crdScalePaths struct {
	specReplicas   string
	statusReplicas string
	labelSelector  string
}

// crdSubresourceMetadata reads status and scale declarations without exposing arbitrary CRD data.
func crdSubresourceMetadata(spec map[string]interface{}) (bool, bool, crdScalePaths) {
	result := crdScalePaths{}
	subresources, found, err := unstructured.NestedMap(spec, "subresources")
	if err != nil || !found {
		return false, false, result
	}
	_, statusFound := subresources["status"]
	rawScale, scaleDeclared := subresources["scale"]
	if !scaleDeclared {
		return statusFound, false, result
	}
	scale, scaleFound := rawScale.(map[string]interface{})
	if !scaleFound {
		return statusFound, true, result
	}
	result.specReplicas = safeCRDPathFromMap(scale, "specReplicasPath")
	result.statusReplicas = safeCRDPathFromMap(scale, "statusReplicasPath")
	result.labelSelector = safeCRDPathFromMap(scale, "labelSelectorPath")
	return statusFound, true, result
}

// safeCRDPathFromMap returns a canonical path only when a scale path is safely tokenized.
func safeCRDPathFromMap(object map[string]interface{}, key string) string {
	value, ok := object[key].(string)
	if !ok {
		return ""
	}
	_, canonical, ok := parseSafeCRDFieldPath(value)
	if !ok {
		return ""
	}
	return canonical
}

// parseCRDPrinterColumns keeps only bounded scalar columns with simple field paths.
func parseCRDPrinterColumns(versionObject, spec map[string]interface{}) ([]CustomResourcePrinterColumn, []string) {
	rawColumns, found := versionObject["additionalPrinterColumns"]
	if !found {
		rawColumns, found = spec["additionalPrinterColumns"]
	}
	if !found || rawColumns == nil {
		return nil, nil
	}
	columns, ok := rawColumns.([]interface{})
	if !ok {
		return nil, []string{"printer columns were ignored because the CRD declaration is malformed"}
	}
	result := make([]CustomResourcePrinterColumn, 0, minInt(len(columns), maxCRDPrinterColumns))
	warnings := make([]string, 0)
	seenNames := make(map[string]struct{}, len(columns))
	for index, rawColumn := range columns {
		if len(result) >= maxCRDPrinterColumns {
			appendCRDPrinterWarning(&warnings, "additional printer columns were omitted after the safe display limit")
			break
		}
		column, ok := rawColumn.(map[string]interface{})
		if !ok {
			appendCRDPrinterWarning(&warnings, fmt.Sprintf("printer column %d was ignored because it is malformed", index+1))
			continue
		}
		name, nameOK := column["name"].(string)
		columnType, typeOK := column["type"].(string)
		jsonPath, pathOK := column["jsonPath"].(string)
		name = strings.TrimSpace(name)
		columnType = strings.ToLower(strings.TrimSpace(columnType))
		if !nameOK || !safeCRDColumnName(name) || !typeOK || !safeCRDPrinterColumnType(columnType) || !pathOK {
			appendCRDPrinterWarning(&warnings, fmt.Sprintf("printer column %d was ignored because its metadata is unsupported", index+1))
			continue
		}
		if _, duplicate := seenNames[name]; duplicate {
			appendCRDPrinterWarning(&warnings, fmt.Sprintf("duplicate printer column %q was ignored", name))
			continue
		}
		path, _, pathOK := parseSafeCRDFieldPath(jsonPath)
		if !pathOK {
			appendCRDPrinterWarning(&warnings, fmt.Sprintf("printer column %q was ignored because its JSONPath is unsupported", name))
			continue
		}
		description, _, _ := unstructured.NestedString(column, "description")
		priority := int32(0)
		if rawPriority, foundPriority := column["priority"]; foundPriority {
			parsedPriority, priorityOK := boundedCRDInt32(rawPriority)
			if !priorityOK || parsedPriority < 0 {
				appendCRDPrinterWarning(&warnings, fmt.Sprintf("printer column %q was ignored because its priority is invalid", name))
				continue
			}
			priority = parsedPriority
		}
		seenNames[name] = struct{}{}
		result = append(result, CustomResourcePrinterColumn{Name: name, Type: columnType, Description: description, Priority: priority, Path: path})
	}
	return result, warnings
}

// appendCRDPrinterWarning adds one safe warning without allowing malformed CRDs to grow responses unboundedly.
func appendCRDPrinterWarning(warnings *[]string, warning string) {
	if warnings == nil || len(*warnings) >= maxCRDPrinterColumnWarnings {
		return
	}
	*warnings = append(*warnings, warning)
}

// boundedCRDInt32 converts JSON numeric values into non-overflowing printer priorities.
func boundedCRDInt32(value interface{}) (int32, bool) {
	var number int64
	switch typed := value.(type) {
	case int:
		number = int64(typed)
	case int32:
		number = int64(typed)
	case int64:
		number = typed
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < 0 || typed > float64(1<<31-1) || typed != math.Trunc(typed) {
			return 0, false
		}
		number = int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	if number < 0 || number > 1<<31-1 {
		return 0, false
	}
	return int32(number), true
}

// safeCRDColumnName validates the bounded label used for a custom table column.
func safeCRDColumnName(value string) bool {
	if value == "" || len(value) > maxCRDPrinterColumnName {
		return false
	}
	for _, character := range value {
		if character == '\x00' || character == '\r' || character == '\n' || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// safeCRDPrinterColumnType reports whether a printer column can be rendered as a scalar.
func safeCRDPrinterColumnType(value string) bool {
	switch value {
	case "string", "integer", "number", "boolean", "date":
		return true
	default:
		return false
	}
}

// safeCRDCoordinate validates a CRD group or version coordinate without parsing arbitrary data.
func safeCRDCoordinate(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 253 && !strings.ContainsAny(value, "\x00\r\n\t")
}

// parseSafeCRDFieldPath accepts only dotted object keys and returns a canonical path.
func parseSafeCRDFieldPath(value string) ([]string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxCRDPrinterColumnPath || !strings.HasPrefix(value, ".") {
		return nil, "", false
	}
	parts := strings.Split(strings.TrimPrefix(value, "."), ".")
	if len(parts) == 0 || len(parts) > maxCRDPrinterColumnSegments {
		return nil, "", false
	}
	for _, part := range parts {
		if part == "" || len(part) > 253 {
			return nil, "", false
		}
		for _, character := range part {
			if !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '-') {
				return nil, "", false
			}
		}
	}
	return parts, "." + strings.Join(parts, "."), true
}

// containsString reports whether one bounded string slice contains a value.
func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// minInt returns the smaller integer without importing a generic helper for one bound.
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// cloneCustomResourceCapabilities returns an isolated copy for cached catalog responses.
func cloneCustomResourceCapabilities(capabilities *CustomResourceCapabilities) *CustomResourceCapabilities {
	if capabilities == nil {
		return nil
	}
	cloned := *capabilities
	cloned.ServedVersions = append([]string(nil), capabilities.ServedVersions...)
	cloned.PrinterColumnWarnings = append([]string(nil), capabilities.PrinterColumnWarnings...)
	cloned.PrinterColumns = make([]CustomResourcePrinterColumn, len(capabilities.PrinterColumns))
	for index, column := range capabilities.PrinterColumns {
		cloned.PrinterColumns[index] = column
		cloned.PrinterColumns[index].Path = append([]string(nil), column.Path...)
	}
	return &cloned
}

// evaluateCustomResourcePrinterColumns extracts bounded scalar values for a custom-resource list row.
func evaluateCustomResourcePrinterColumns(item *unstructured.Unstructured, capabilities *CustomResourceCapabilities) map[string]string {
	if item == nil || capabilities == nil || len(capabilities.PrinterColumns) == 0 {
		return nil
	}
	values := make(map[string]string, len(capabilities.PrinterColumns))
	for _, column := range capabilities.PrinterColumns {
		values[column.Name] = safePrinterColumnValue(item.Object, column)
	}
	return values
}

// safePrinterColumnValue evaluates only pre-tokenized map paths and bounded scalar values.
func safePrinterColumnValue(object map[string]interface{}, column CustomResourcePrinterColumn) string {
	var current interface{} = object
	for _, segment := range column.Path {
		values, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current, ok = values[segment]
		if !ok {
			return ""
		}
	}
	value, ok := formatSafePrinterColumnValue(current, column.Type)
	if !ok || len(value) > maxCRDPrinterColumnValue {
		if len(value) > maxCRDPrinterColumnValue {
			return value[:maxCRDPrinterColumnValue]
		}
		return ""
	}
	return value
}

// formatSafePrinterColumnValue renders only scalar JSON values in the declared column type.
func formatSafePrinterColumnValue(value interface{}, columnType string) (string, bool) {
	switch columnType {
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return "", false
		}
		return strconv.FormatBool(boolean), true
	case "integer":
		switch number := value.(type) {
		case int, int32, int64:
			return fmt.Sprint(number), true
		case float64:
			if math.IsNaN(number) || math.IsInf(number, 0) || number < -float64(1<<63) || number >= float64(1<<63-1) || number != math.Trunc(number) {
				return "", false
			}
			return strconv.FormatInt(int64(number), 10), true
		default:
			return "", false
		}
	case "number":
		switch number := value.(type) {
		case int, int32, int64, float32:
			return fmt.Sprint(number), true
		case float64:
			if math.IsNaN(number) || math.IsInf(number, 0) {
				return "", false
			}
			return fmt.Sprint(number), true
		default:
			return "", false
		}
	case "string", "date":
		text, ok := value.(string)
		return text, ok
	default:
		return "", false
	}
}
