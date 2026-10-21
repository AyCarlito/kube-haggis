package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	readPackRequestDeadline = 2 * time.Second
	readPackListLimit       = int64(50)
	readPackResultLimit     = 100
	readPackScalarLimit     = 512
	readPackFieldLimit      = 32
	readPackContainerLimit  = 32
	readPackVolumeLimit     = 32
	readPackRuleLimit       = 32
	readPackBindingLimit    = 50

	readPackStateAvailable        = "available"
	readPackStateEmpty            = "empty"
	readPackStatePartial          = "partial"
	readPackStatePermissionDenied = "permission-denied"
	readPackStateUnavailable      = "unavailable"
	readPackStateError            = "error"
	readPackStateStale            = "stale"

	readPackPanelIngress          = "ingress"
	readPackPanelEndpointSlice    = "endpoint-slice"
	readPackPanelNetworkPolicy    = "network-policy"
	readPackPanelHorizontalScaler = "horizontal-pod-autoscaler"
	readPackPanelDisruptionBudget = "pod-disruption-budget"
	readPackPanelResourceQuota    = "resource-quota"
	readPackPanelLimitRange       = "limit-range"
	readPackPanelPersistentVolume = "persistent-volume"
	readPackPanelStorageClass     = "storage-class"
	readPackPanelServiceAccount   = "service-account"
	readPackPanelPod              = "pod"
	readPackPanelCompute          = "compute"
)

// OperationalReadPack is the bounded, read-only summary of one built-in resource.
type OperationalReadPack struct {
	State       string                         `json:"state"`
	Message     string                         `json:"message"`
	Panels      []OperationalReadPackPanel     `json:"panels"`
	Diagnostics OperationalReadPackDiagnostics `json:"diagnostics"`
}

// OperationalReadPackPanel describes one independently readable operational view.
type OperationalReadPackPanel struct {
	Key       string      `json:"key"`
	Title     string      `json:"title"`
	State     string      `json:"state"`
	Message   string      `json:"message"`
	Truncated bool        `json:"truncated,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// OperationalReadPackDiagnostics contains safe call counts and timing limits for a read pack.
type OperationalReadPackDiagnostics struct {
	QueryCount           int   `json:"queryCount"`
	QueryBudget          int   `json:"queryBudget"`
	DurationMilliseconds int64 `json:"durationMilliseconds"`
	DeadlineMilliseconds int64 `json:"deadlineMilliseconds"`
	ResultLimit          int   `json:"resultLimit"`
}

// OperationalReadPackReference identifies a related object without returning its body.
type OperationalReadPackReference struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	State      string `json:"state"`
	Message    string `json:"message,omitempty"`
}

// OperationalCondition is a safe Kubernetes condition summary.
type OperationalCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// OperationalLabelExpression describes one bounded selector requirement.
type OperationalLabelExpression struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// OperationalLabelSelector is a display-safe native Kubernetes label selector.
type OperationalLabelSelector struct {
	MatchLabels      map[string]string            `json:"matchLabels"`
	MatchExpressions []OperationalLabelExpression `json:"matchExpressions"`
	Text             string                       `json:"text"`
}

// IngressReadPackData summarizes ingress rules and discovered service backends.
type IngressReadPackData struct {
	IngressClass   string                         `json:"ingressClass,omitempty"`
	TLSHosts       []string                       `json:"tlsHosts"`
	Rules          []IngressRuleSummary           `json:"rules"`
	Backends       []IngressBackendSummary        `json:"backends"`
	Services       []OperationalReadPackReference `json:"services"`
	EndpointSlices []EndpointSliceSummary         `json:"endpointSlices"`
}

// IngressRuleSummary contains one host and its bounded HTTP paths.
type IngressRuleSummary struct {
	Host  string               `json:"host,omitempty"`
	Paths []IngressPathSummary `json:"paths"`
}

// IngressPathSummary identifies one ingress service backend without exposing object bodies.
type IngressPathSummary struct {
	Path        string `json:"path,omitempty"`
	PathType    string `json:"pathType,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	ServicePort string `json:"servicePort,omitempty"`
	Resource    string `json:"resource,omitempty"`
}

// IngressBackendSummary identifies one unique backend target.
type IngressBackendSummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Port      string `json:"port,omitempty"`
	Kind      string `json:"kind,omitempty"`
	State     string `json:"state"`
}

// EndpointSliceSummary contains safe endpoint addresses and readiness flags.
type EndpointSliceSummary struct {
	Name        string            `json:"name"`
	ServiceName string            `json:"serviceName,omitempty"`
	AddressType string            `json:"addressType,omitempty"`
	Ports       []string          `json:"ports"`
	Endpoints   []EndpointSummary `json:"endpoints"`
}

// EndpointSliceReadPackData contains the current EndpointSlice summary.
type EndpointSliceReadPackData struct {
	Slice EndpointSliceSummary `json:"slice"`
}

// EndpointSummary contains one endpoint's addresses and safe topology state.
type EndpointSummary struct {
	Addresses   []string `json:"addresses"`
	Ready       *bool    `json:"ready,omitempty"`
	Serving     *bool    `json:"serving,omitempty"`
	Terminating *bool    `json:"terminating,omitempty"`
	NodeName    string   `json:"nodeName,omitempty"`
	Zone        string   `json:"zone,omitempty"`
}

// NetworkPolicyReadPackData summarizes policy selectors, rule shapes, and affected Pods.
type NetworkPolicyReadPackData struct {
	PodSelector  OperationalLabelSelector       `json:"podSelector"`
	PolicyTypes  []string                       `json:"policyTypes"`
	IngressRules []NetworkPolicyRuleSummary     `json:"ingressRules"`
	EgressRules  []NetworkPolicyRuleSummary     `json:"egressRules"`
	AffectedPods []OperationalReadPackReference `json:"affectedPods"`
}

// NetworkPolicyRuleSummary describes peer and port counts without copying provider fields.
type NetworkPolicyRuleSummary struct {
	PeerCount int      `json:"peerCount"`
	PortCount int      `json:"portCount"`
	Peers     []string `json:"peers"`
	Ports     []string `json:"ports"`
}

// HorizontalPodAutoscalerReadPackData summarizes target, scaling bounds, metrics, and status.
type HorizontalPodAutoscalerReadPackData struct {
	Target          OperationalReadPackReference `json:"target"`
	MinReplicas     *int64                       `json:"minReplicas,omitempty"`
	MaxReplicas     *int64                       `json:"maxReplicas,omitempty"`
	CurrentReplicas *int64                       `json:"currentReplicas,omitempty"`
	DesiredReplicas *int64                       `json:"desiredReplicas,omitempty"`
	Metrics         []AutoscalerMetricSummary    `json:"metrics"`
	Conditions      []OperationalCondition       `json:"conditions"`
}

// AutoscalerMetricSummary describes one HPA metric target and current reading.
type AutoscalerMetricSummary struct {
	Type              string `json:"type"`
	Resource          string `json:"resource,omitempty"`
	Name              string `json:"name,omitempty"`
	Current           string `json:"current,omitempty"`
	Target            string `json:"target,omitempty"`
	TargetAverage     string `json:"targetAverage,omitempty"`
	TargetUtilization *int64 `json:"targetUtilization,omitempty"`
}

// PodDisruptionBudgetReadPackData summarizes availability and disruption allowance.
type PodDisruptionBudgetReadPackData struct {
	MinAvailable       string                 `json:"minAvailable,omitempty"`
	MaxUnavailable     string                 `json:"maxUnavailable,omitempty"`
	ExpectedPods       *int64                 `json:"expectedPods,omitempty"`
	CurrentHealthy     *int64                 `json:"currentHealthy,omitempty"`
	DesiredHealthy     *int64                 `json:"desiredHealthy,omitempty"`
	DisruptionsAllowed *int64                 `json:"disruptionsAllowed,omitempty"`
	Conditions         []OperationalCondition `json:"conditions"`
}

// ResourceQuotaReadPackData summarizes hard and used quota quantities.
type ResourceQuotaReadPackData struct {
	Scopes []string            `json:"scopes"`
	Usage  []QuotaUsageSummary `json:"usage"`
}

// QuotaUsageSummary contains one resource quota's used, hard, and optional percentage values.
type QuotaUsageSummary struct {
	Resource string  `json:"resource"`
	Used     string  `json:"used,omitempty"`
	Hard     string  `json:"hard,omitempty"`
	Percent  *uint64 `json:"percent,omitempty"`
}

// LimitRangeReadPackData summarizes bounded default, request, min, and max quantities.
type LimitRangeReadPackData struct {
	Items []LimitRangeItemSummary `json:"items"`
}

// LimitRangeItemSummary contains one LimitRange item without arbitrary provider fields.
type LimitRangeItemSummary struct {
	Type           string            `json:"type"`
	Default        map[string]string `json:"default"`
	DefaultRequest map[string]string `json:"defaultRequest"`
	Max            map[string]string `json:"max"`
	Min            map[string]string `json:"min"`
}

// PersistentVolumeReadPackData summarizes PV lifecycle fields and safe binding references.
type PersistentVolumeReadPackData struct {
	Phase           string                         `json:"phase,omitempty"`
	Reason          string                         `json:"reason,omitempty"`
	StorageClass    string                         `json:"storageClass,omitempty"`
	ReclaimPolicy   string                         `json:"reclaimPolicy,omitempty"`
	VolumeMode      string                         `json:"volumeMode,omitempty"`
	Capacity        map[string]string              `json:"capacity"`
	ClaimRef        OperationalReadPackReference   `json:"claimRef"`
	Source          string                         `json:"source,omitempty"`
	NodeAffinity    bool                           `json:"nodeAffinity"`
	ProviderMessage bool                           `json:"providerMessage"`
	Relationships   []OperationalReadPackReference `json:"relationships"`
}

// StorageClassReadPackData summarizes provisioning behavior and bound PV relationships.
type StorageClassReadPackData struct {
	Provisioner          string                         `json:"provisioner,omitempty"`
	ReclaimPolicy        string                         `json:"reclaimPolicy,omitempty"`
	VolumeBindingMode    string                         `json:"volumeBindingMode,omitempty"`
	AllowVolumeExpansion *bool                          `json:"allowVolumeExpansion,omitempty"`
	MountOptions         []string                       `json:"mountOptions"`
	AllowedTopologies    int                            `json:"allowedTopologies"`
	PersistentVolumes    []OperationalReadPackReference `json:"persistentVolumes"`
}

// ServiceAccountReadPackData summarizes token references and read-only RBAC bindings.
type ServiceAccountReadPackData struct {
	AutomountServiceAccountToken *bool                `json:"automountServiceAccountToken,omitempty"`
	Secrets                      []string             `json:"secrets"`
	ImagePullSecrets             []string             `json:"imagePullSecrets"`
	RoleBindings                 []RBACBindingSummary `json:"roleBindings"`
	ClusterRoleBindings          []RBACBindingSummary `json:"clusterRoleBindings"`
}

// RBACBindingSummary identifies a RoleBinding or ClusterRoleBinding and its safe subjects.
type RBACBindingSummary struct {
	Name      string               `json:"name"`
	Namespace string               `json:"namespace,omitempty"`
	RoleKind  string               `json:"roleKind,omitempty"`
	RoleName  string               `json:"roleName,omitempty"`
	Subjects  []RBACSubjectSummary `json:"subjects"`
}

// RBACSubjectSummary contains one non-secret RBAC subject identity.
type RBACSubjectSummary struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// PodReadPackData summarizes scheduling, readiness, probes, termination, volumes, and security.
type PodReadPackData struct {
	Phase               string                    `json:"phase,omitempty"`
	Reason              string                    `json:"reason,omitempty"`
	NodeName            string                    `json:"nodeName,omitempty"`
	SchedulerName       string                    `json:"schedulerName,omitempty"`
	PodIP               string                    `json:"podIP,omitempty"`
	Conditions          []OperationalCondition    `json:"conditions"`
	ReadinessGates      []string                  `json:"readinessGates"`
	Containers          []PodContainerSummary     `json:"containers"`
	InitContainers      []PodContainerSummary     `json:"initContainers"`
	EphemeralContainers []PodContainerSummary     `json:"ephemeralContainers"`
	Volumes             []PodVolumeSummary        `json:"volumes"`
	SecurityContext     PodSecurityContextSummary `json:"securityContext"`
}

// PodContainerSummary contains image, readiness, probes, restarts, and termination reason data.
type PodContainerSummary struct {
	Name                    string                      `json:"name"`
	Image                   string                      `json:"image,omitempty"`
	Ready                   *bool                       `json:"ready,omitempty"`
	Started                 *bool                       `json:"started,omitempty"`
	RestartCount            *int64                      `json:"restartCount,omitempty"`
	State                   string                      `json:"state,omitempty"`
	StateReason             string                      `json:"stateReason,omitempty"`
	StateExitCode           *int64                      `json:"stateExitCode,omitempty"`
	LastTerminationReason   string                      `json:"lastTerminationReason,omitempty"`
	LastTerminationExitCode *int64                      `json:"lastTerminationExitCode,omitempty"`
	Probes                  []PodProbeSummary           `json:"probes"`
	SecurityContext         PodContainerSecuritySummary `json:"securityContext"`
}

// PodProbeSummary identifies configured liveness, readiness, and startup probe handlers.
type PodProbeSummary struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	Handler    string `json:"handler,omitempty"`
}

// PodVolumeSummary identifies a volume source by type and safe object name only.
type PodVolumeSummary struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Reference string `json:"reference,omitempty"`
	ReadOnly  *bool  `json:"readOnly,omitempty"`
}

// PodSecurityContextSummary contains non-secret Pod-level security settings.
type PodSecurityContextSummary struct {
	RunAsUser          *int64 `json:"runAsUser,omitempty"`
	RunAsGroup         *int64 `json:"runAsGroup,omitempty"`
	RunAsNonRoot       *bool  `json:"runAsNonRoot,omitempty"`
	FSGroup            *int64 `json:"fsGroup,omitempty"`
	SeccompProfileType string `json:"seccompProfileType,omitempty"`
	HostNetwork        *bool  `json:"hostNetwork,omitempty"`
	HostPID            *bool  `json:"hostPID,omitempty"`
	HostIPC            *bool  `json:"hostIPC,omitempty"`
}

// PodContainerSecuritySummary contains non-secret container security settings.
type PodContainerSecuritySummary struct {
	Privileged               *bool    `json:"privileged,omitempty"`
	AllowPrivilegeEscalation *bool    `json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool    `json:"readOnlyRootFilesystem,omitempty"`
	RunAsUser                *int64   `json:"runAsUser,omitempty"`
	RunAsNonRoot             *bool    `json:"runAsNonRoot,omitempty"`
	SeccompProfileType       string   `json:"seccompProfileType,omitempty"`
	Capabilities             []string `json:"capabilities"`
}

// ComputeReadPackData contains bounded object-side quantities and native usage for one Node or Pod.
type ComputeReadPackData struct {
	Kind        string              `json:"kind"`
	Name        string              `json:"name"`
	Namespace   string              `json:"namespace,omitempty"`
	Requests    ComputeQuantitySet  `json:"requests"`
	Limits      ComputeQuantitySet  `json:"limits"`
	Capacity    ComputeQuantitySet  `json:"capacity"`
	Allocatable ComputeQuantitySet  `json:"allocatable"`
	Usage       ComputeUsageSummary `json:"usage"`
}

// ComputeQuantitySet contains normalized CPU and memory quantities with per-value completeness states.
type ComputeQuantitySet struct {
	CPU     ComputeQuantityValue `json:"cpu"`
	Memory  ComputeQuantityValue `json:"memory"`
	State   string               `json:"state"`
	Message string               `json:"message,omitempty"`
}

// ComputeQuantityValue contains one bounded Kubernetes quantity and its semantic state.
type ComputeQuantityValue struct {
	Value string `json:"value,omitempty"`
	State string `json:"state"`
}

// ComputeUsageSummary contains normalized metrics.k8s.io usage and its sample freshness state.
type ComputeUsageSummary struct {
	CPU       ComputeQuantityValue `json:"cpu"`
	Memory    ComputeQuantityValue `json:"memory"`
	State     string               `json:"state"`
	Message   string               `json:"message,omitempty"`
	Timestamp *time.Time           `json:"timestamp,omitempty"`
}

// readPackListResult carries one capped generic list and its safe truncation flag.
type readPackListResult struct {
	Items     *unstructured.UnstructuredList
	Truncated bool
}

// readPackFieldResult records whether a generic field was found and whether its shape was malformed.
type readPackFieldResult[T any] struct {
	Value     T
	Found     bool
	Malformed bool
}

// readPackAggregateBudget bounds the combined output of nested collections in one panel.
type readPackAggregateBudget struct {
	limit int
	used  int
}

// newReadPackAggregateBudget creates a deterministic panel-wide output budget.
func newReadPackAggregateBudget(limit int) *readPackAggregateBudget {
	return &readPackAggregateBudget{limit: limit}
}

// take admits one emitted nested value when the panel still has aggregate capacity.
func (budget *readPackAggregateBudget) take() bool {
	if budget.used >= budget.limit {
		return false
	}
	budget.used++
	return true
}

// exhausted reports whether the panel-wide nested output budget is full.
func (budget *readPackAggregateBudget) exhausted() bool {
	return budget.used >= budget.limit
}

// OperationalReadPack returns one exact-GVR operational read pack using bounded generic reads.
func (s *Service) OperationalReadPack(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (OperationalReadPack, error) {
	started := time.Now()
	requestContext, cancel := context.WithTimeout(ctx, readPackRequestDeadline)
	defer cancel()

	dynamicClient, discoveryClient, _, resolvedContext, revision, err := s.clientsWithRevision(contextName)
	if err != nil {
		return OperationalReadPack{}, err
	}
	catalog, _ := s.cachedDiscoveryCatalog(requestContext, resolvedContext, revision, dynamicClient, discoveryClient)
	definition, found := exactResourceDefinition(catalog.Items, group, version, resource)
	if !found {
		return unavailableOperationalReadPack(
			operationalPanelKey(ResourceDefinition{Group: group, Version: version, Resource: resource}),
			operationalPanelTitle(ResourceDefinition{Group: group, Version: version, Resource: resource}),
			"This exact group/version/resource is not available in discovery; generic YAML, Events, and related resources remain available.",
			OperationalReadPackDiagnostics{DurationMilliseconds: time.Since(started).Milliseconds(), DeadlineMilliseconds: readPackRequestDeadline.Milliseconds(), ResultLimit: readPackResultLimit},
		), nil
	}
	if definition.Namespaced != namespaced {
		return OperationalReadPack{}, fmt.Errorf("resource scope contradicts discovery")
	}
	if err := validateResourceNamespace(definition, namespace, true); err != nil {
		return OperationalReadPack{}, err
	}
	budget := newRelatedCallBudget(requestContext)
	resourceClient, err := resourceInterface(dynamicClient, schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, namespace, namespaced, true)
	if err != nil {
		return OperationalReadPack{}, err
	}
	var object *unstructured.Unstructured
	err = budget.call(func() error {
		var getErr error
		object, getErr = resourceClient.Get(requestContext, name, metav1.GetOptions{})
		return getErr
	})
	diagnostics := operationalReadPackDiagnostics(budget, started)
	if err != nil {
		return failedOperationalReadPack(definition, operationalReadPackErrorState(err), operationalReadPackErrorMessage(err), diagnostics), nil
	}
	return buildOperationalReadPack(requestContext, dynamicClient, catalog.Items, definition, object, budget, started), nil
}

// buildOperationalReadPack extracts one sanitized panel and any fixed-family relationships.
func buildOperationalReadPack(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget, started time.Time) OperationalReadPack {
	panel := operationalPanelForResource(ctx, client, definitions, definition, object, budget)
	panels := []OperationalReadPackPanel{panel}
	if definition.Kind == "Pod" && supportedOperationalVersion(definition) {
		panels = append(panels, extractComputeReadPack(ctx, client, definition, object, budget))
	}
	pack := OperationalReadPack{Panels: panels, Diagnostics: operationalReadPackDiagnostics(budget, started)}
	pack.State, pack.Message = summarizeOperationalPackState(pack.Panels)
	return pack
}

// operationalPanelForResource chooses the fixed read-pack extractor for one native kind.
func operationalPanelForResource(ctx context.Context, client dynamic.Interface, definitions []ResourceDefinition, definition ResourceDefinition, object *unstructured.Unstructured, budget *relatedCallBudget) OperationalReadPackPanel {
	key := operationalPanelKey(definition)
	title := operationalPanelTitle(definition)
	if key == "" {
		return newOperationalReadPackPanel("generic", "Built-in operational view", readPackStateUnavailable, "This resource has no built-in operational read pack; generic YAML, Events, and related resources remain available.", nil, false)
	}
	if !supportedOperationalVersion(definition) {
		return newOperationalReadPackPanel(key, title, readPackStateUnavailable, "This API version is not supported by the structured read pack; generic YAML, Events, and related resources remain available.", nil, false)
	}
	switch key {
	case readPackPanelIngress:
		return extractIngressReadPack(ctx, client, definitions, definition, object, budget)
	case readPackPanelEndpointSlice:
		return extractEndpointSliceReadPack(definition, object)
	case readPackPanelNetworkPolicy:
		return extractNetworkPolicyReadPack(ctx, client, definitions, definition, object, budget)
	case readPackPanelHorizontalScaler:
		return extractHorizontalPodAutoscalerReadPack(definition, object)
	case readPackPanelDisruptionBudget:
		return extractPodDisruptionBudgetReadPack(definition, object)
	case readPackPanelResourceQuota:
		return extractResourceQuotaReadPack(definition, object)
	case readPackPanelLimitRange:
		return extractLimitRangeReadPack(definition, object)
	case readPackPanelPersistentVolume:
		return extractPersistentVolumeReadPack(ctx, client, definitions, definition, object, budget)
	case readPackPanelStorageClass:
		return extractStorageClassReadPack(ctx, client, definitions, definition, object, budget)
	case readPackPanelServiceAccount:
		return extractServiceAccountReadPack(ctx, client, definitions, definition, object, budget)
	case readPackPanelPod:
		return extractPodReadPack(definition, object)
	case readPackPanelCompute:
		return extractComputeReadPack(ctx, client, definition, object, budget)
	default:
		return newOperationalReadPackPanel(key, title, readPackStateUnavailable, "This built-in operational view is not available for the requested resource.", nil, false)
	}
}

// operationalPanelKey returns the stable panel key for a discovered native resource.
func operationalPanelKey(definition ResourceDefinition) string {
	switch definition.Kind {
	case "Ingress":
		return readPackPanelIngress
	case "EndpointSlice":
		return readPackPanelEndpointSlice
	case "NetworkPolicy":
		return readPackPanelNetworkPolicy
	case "HorizontalPodAutoscaler":
		return readPackPanelHorizontalScaler
	case "PodDisruptionBudget":
		return readPackPanelDisruptionBudget
	case "ResourceQuota":
		return readPackPanelResourceQuota
	case "LimitRange":
		return readPackPanelLimitRange
	case "PersistentVolume":
		return readPackPanelPersistentVolume
	case "StorageClass":
		return readPackPanelStorageClass
	case "ServiceAccount":
		return readPackPanelServiceAccount
	case "Pod":
		return readPackPanelPod
	case "Node":
		return readPackPanelCompute
	default:
		return ""
	}
}

// operationalPanelTitle returns the field-console title for one built-in panel.
func operationalPanelTitle(definition ResourceDefinition) string {
	switch operationalPanelKey(definition) {
	case readPackPanelIngress:
		return "Ingress backends"
	case readPackPanelEndpointSlice:
		return "EndpointSlice endpoints"
	case readPackPanelNetworkPolicy:
		return "Network policy coverage"
	case readPackPanelHorizontalScaler:
		return "Autoscaler status"
	case readPackPanelDisruptionBudget:
		return "Disruption allowance"
	case readPackPanelResourceQuota:
		return "Quota usage"
	case readPackPanelLimitRange:
		return "Limit defaults"
	case readPackPanelPersistentVolume:
		return "Volume lifecycle"
	case readPackPanelStorageClass:
		return "Storage provisioning"
	case readPackPanelServiceAccount:
		return "Service account access"
	case readPackPanelPod:
		return "Pod posture"
	case readPackPanelCompute:
		return "Compute"
	default:
		return "Operational view"
	}
}

// supportedOperationalVersion reports whether the exact built-in GVR has a safe extractor.
func supportedOperationalVersion(definition ResourceDefinition) bool {
	switch definition.Kind {
	case "Ingress", "NetworkPolicy":
		return definition.Group == "networking.k8s.io" && definition.Version == "v1"
	case "EndpointSlice":
		return definition.Group == "discovery.k8s.io" && (definition.Version == "v1" || definition.Version == "v1beta1")
	case "HorizontalPodAutoscaler":
		return definition.Group == "autoscaling" && (definition.Version == "v1" || definition.Version == "v2" || definition.Version == "v2beta2")
	case "PodDisruptionBudget":
		return definition.Group == "policy" && (definition.Version == "v1" || definition.Version == "v1beta1")
	case "ResourceQuota", "LimitRange", "PersistentVolume", "ServiceAccount", "Pod", "Node":
		return definition.Group == "" && definition.Version == "v1"
	case "StorageClass":
		return definition.Group == "storage.k8s.io" && definition.Version == "v1"
	default:
		return false
	}
}

// exactResourceDefinition finds one exact catalog GVR without falling back to a short name.
func exactResourceDefinition(definitions []ResourceDefinition, group, version, resource string) (ResourceDefinition, bool) {
	for _, definition := range definitions {
		if definition.Group == group && definition.Version == version && definition.Resource == resource {
			return definition, true
		}
	}
	return ResourceDefinition{}, false
}

// newOperationalReadPackPanel creates a JSON-stable panel with explicit state and safe copy.
func newOperationalReadPackPanel(key, title, state, message string, data interface{}, truncated bool) OperationalReadPackPanel {
	return OperationalReadPackPanel{Key: key, Title: title, State: state, Message: message, Data: data, Truncated: truncated}
}

// failedOperationalReadPack creates a panel-only response after the current object cannot be read.
func failedOperationalReadPack(definition ResourceDefinition, state, message string, diagnostics OperationalReadPackDiagnostics) OperationalReadPack {
	key := operationalPanelKey(definition)
	if key == "" {
		key = "generic"
	}
	panel := newOperationalReadPackPanel(key, operationalPanelTitle(definition), state, message, nil, false)
	return OperationalReadPack{State: state, Message: message, Panels: []OperationalReadPackPanel{panel}, Diagnostics: diagnostics}
}

// unavailableOperationalReadPack creates a safe response when discovery cannot prove a panel exists.
func unavailableOperationalReadPack(key, title, message string, diagnostics OperationalReadPackDiagnostics) OperationalReadPack {
	if key == "" {
		key = "generic"
	}
	return OperationalReadPack{State: readPackStateUnavailable, Message: message, Panels: []OperationalReadPackPanel{newOperationalReadPackPanel(key, title, readPackStateUnavailable, message, nil, false)}, Diagnostics: diagnostics}
}

// operationalReadPackDiagnostics converts the shared bounded call budget into public diagnostics.
func operationalReadPackDiagnostics(budget *relatedCallBudget, started time.Time) OperationalReadPackDiagnostics {
	return OperationalReadPackDiagnostics{
		QueryCount:           budget.count(),
		QueryBudget:          budget.limit,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		DeadlineMilliseconds: readPackRequestDeadline.Milliseconds(),
		ResultLimit:          readPackResultLimit,
	}
}

// summarizeOperationalPackState derives the safe aggregate state from independent panels.
func summarizeOperationalPackState(panels []OperationalReadPackPanel) (string, string) {
	if len(panels) == 0 {
		return readPackStateUnavailable, "No built-in operational panel is available for this resource."
	}
	state := panels[0].State
	for _, panel := range panels[1:] {
		if panel.State != state {
			return readPackStatePartial, "The operational view contains explicit panel limitations; generic YAML, Events, and related resources remain available."
		}
	}
	switch state {
	case readPackStateAvailable:
		return state, "Structured read-only operational data is available."
	case readPackStateEmpty:
		return state, "The operational API is readable, but it returned no rows for this view."
	case readPackStatePermissionDenied:
		return state, "Kubernetes RBAC denied this operational read."
	case readPackStateUnavailable:
		return state, "This operational API or exact version is unavailable; generic views remain available."
	case readPackStateStale:
		return state, "The latest native telemetry sample is stale; object-side fields remain available."
	case readPackStateError:
		return state, "The operational API returned an unreadable result; generic views remain available."
	default:
		return readPackStatePartial, "Some operational fields were incomplete; generic views remain available."
	}
}

// operationalReadPackErrorState maps one Kubernetes read failure to a safe panel state.
func operationalReadPackErrorState(err error) string {
	switch {
	case errors.Is(err, errRelatedCallBudget), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return readPackStatePartial
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return readPackStatePermissionDenied
	case apierrors.IsNotFound(err), apierrors.IsMethodNotSupported(err), apierrors.IsNotAcceptable(err):
		return readPackStateUnavailable
	default:
		return readPackStateError
	}
}

// operationalReadPackErrorMessage converts a Kubernetes error into non-sensitive operator copy.
func operationalReadPackErrorMessage(err error) string {
	switch {
	case errors.Is(err, errRelatedCallBudget):
		return "The operational view reached its safe Kubernetes-call budget; remaining fields were not queried."
	case errors.Is(err, context.DeadlineExceeded):
		return "The operational view reached its safe request deadline; remaining fields were not queried."
	case errors.Is(err, context.Canceled):
		return "The operational view was canceled before all fields were read."
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return "Kubernetes RBAC denied this operational read."
	case apierrors.IsNotFound(err), apierrors.IsMethodNotSupported(err), apierrors.IsNotAcceptable(err):
		return "This operational API or version is not available on the Kubernetes API server."
	default:
		return "This operational panel could not be read from Kubernetes."
	}
}

// operationalMalformedMessage returns the common safe notice for provider-specific field shapes.
func operationalMalformedMessage() string {
	return "Some provider-specific fields were not in a recognized Kubernetes shape; the remaining safe fields are shown."
}

// operationalRelatedMessage returns a safe notice for an optional related family.
func operationalRelatedMessage(definition ResourceDefinition, verb string) string {
	if definition.Kind == "" {
		return "This related API is not present in discovery; the base object fields remain visible."
	}
	if !hasVerb(definition.Verbs, verb) {
		return "Discovery did not advertise the required read operation; the base object fields remain visible."
	}
	return "Kubernetes did not return complete related data; the base object fields remain visible."
}

// operationalDefinitionForVerb checks exact GVR identity, scope, and a discovered read verb.
func operationalDefinitionForVerb(definitions []ResourceDefinition, group, version, resource string, namespaced bool, verb string) (ResourceDefinition, string) {
	definition, found := exactResourceDefinition(definitions, group, version, resource)
	if !found {
		return ResourceDefinition{}, readPackStateUnavailable
	}
	if definition.Namespaced != namespaced {
		return ResourceDefinition{}, readPackStateError
	}
	if !hasVerb(definition.Verbs, verb) {
		return definition, readPackStateUnavailable
	}
	return definition, readPackStateAvailable
}

// operationalDefinitionForVersions selects the first exact discovered version from a bounded preference list.
func operationalDefinitionForVersions(definitions []ResourceDefinition, group, resource string, namespaced bool, verb string, versions ...string) (ResourceDefinition, string) {
	for _, version := range versions {
		definition, state := operationalDefinitionForVerb(definitions, group, version, resource, namespaced, verb)
		if state == readPackStateAvailable {
			return definition, state
		}
	}
	if len(versions) == 0 {
		return ResourceDefinition{}, readPackStateUnavailable
	}
	definition, state := operationalDefinitionForVerb(definitions, group, versions[0], resource, namespaced, verb)
	return definition, state
}

// listOperationalResource performs one fixed-family list under the shared bounded relationship budget.
func listOperationalResource(ctx context.Context, client dynamic.Interface, budget *relatedCallBudget, definition ResourceDefinition, namespace, labelSelector string) (readPackListResult, error) {
	resourceClient, err := resourceInterface(client, schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}, namespace, definition.Namespaced, false)
	if err != nil {
		return readPackListResult{}, err
	}
	var items *unstructured.UnstructuredList
	err = budget.call(func() error {
		var listErr error
		items, listErr = resourceClient.List(ctx, metav1.ListOptions{Limit: readPackListLimit, LabelSelector: labelSelector})
		return listErr
	})
	if err != nil {
		return readPackListResult{}, err
	}
	return readPackListResult{Items: items, Truncated: items.GetContinue() != ""}, nil
}

// getOperationalResource performs one fixed-family GET without returning the target body.
func getOperationalResource(ctx context.Context, client dynamic.Interface, budget *relatedCallBudget, definition ResourceDefinition, namespace, name string) (*unstructured.Unstructured, error) {
	resourceClient, err := resourceInterface(client, schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}, namespace, definition.Namespaced, false)
	if err != nil {
		return nil, err
	}
	var object *unstructured.Unstructured
	err = budget.call(func() error {
		var getErr error
		object, getErr = resourceClient.Get(ctx, name, metav1.GetOptions{})
		return getErr
	})
	return object, err
}

// referenceFromObject creates a safe navigation reference from an unstructured object.
func referenceFromObject(object *unstructured.Unstructured, definition ResourceDefinition, state string, message string) OperationalReadPackReference {
	if object == nil {
		return OperationalReadPackReference{APIVersion: apiVersionForDefinition(definition), Kind: definition.Kind, State: state, Message: message}
	}
	return OperationalReadPackReference{APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Name: object.GetName(), Namespace: object.GetNamespace(), State: state, Message: message}
}

// referenceForCoordinates creates a safe related-object reference without reading its body.
func referenceForCoordinates(definition ResourceDefinition, namespace, name, state, message string) OperationalReadPackReference {
	return OperationalReadPackReference{APIVersion: apiVersionForDefinition(definition), Kind: definition.Kind, Name: name, Namespace: namespace, State: state, Message: message}
}

// readPackString reads one optional string and records provider-specific shape errors.
func readPackString(object map[string]interface{}, fields ...string) readPackFieldResult[string] {
	value, found, err := unstructured.NestedString(object, fields...)
	if found && len(value) > readPackScalarLimit {
		return readPackFieldResult[string]{Found: true, Malformed: true}
	}
	return readPackFieldResult[string]{Value: value, Found: found, Malformed: err != nil}
}

// readPackBool reads one optional bool and records provider-specific shape errors.
func readPackBool(object map[string]interface{}, fields ...string) readPackFieldResult[bool] {
	value, found, err := unstructured.NestedBool(object, fields...)
	return readPackFieldResult[bool]{Value: value, Found: found, Malformed: err != nil}
}

// readPackInt64 reads one optional integer and records provider-specific shape errors.
func readPackInt64(object map[string]interface{}, fields ...string) readPackFieldResult[int64] {
	value, found, err := unstructured.NestedInt64(object, fields...)
	return readPackFieldResult[int64]{Value: value, Found: found, Malformed: err != nil}
}

// readPackMap reads one optional object map and records provider-specific shape errors.
func readPackMap(object map[string]interface{}, fields ...string) readPackFieldResult[map[string]interface{}] {
	value, found, err := unstructured.NestedMap(object, fields...)
	return readPackFieldResult[map[string]interface{}]{Value: value, Found: found, Malformed: err != nil}
}

// readPackSlice reads one optional list and records provider-specific shape errors.
func readPackSlice(object map[string]interface{}, fields ...string) readPackFieldResult[[]interface{}] {
	value, found, err := unstructured.NestedSlice(object, fields...)
	return readPackFieldResult[[]interface{}]{Value: value, Found: found, Malformed: err != nil}
}

// readPackStringList normalizes a bounded list of safe strings.
func readPackStringList(object map[string]interface{}, fields ...string) ([]string, bool, bool) {
	field := readPackSlice(object, fields...)
	if field.Malformed || !field.Found {
		return []string{}, field.Found, field.Malformed
	}
	result := make([]string, 0, minInt(len(field.Value), readPackFieldLimit))
	partial := false
	for index, raw := range field.Value {
		if index >= readPackFieldLimit {
			partial = true
			break
		}
		value, ok := raw.(string)
		if !ok {
			partial = true
			continue
		}
		if len(value) > readPackScalarLimit {
			partial = true
			continue
		}
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result, true, partial
}

// readPackConditions extracts bounded condition summaries while omitting raw condition messages.
func readPackConditions(object map[string]interface{}, fields ...string) ([]OperationalCondition, bool) {
	field := readPackSlice(object, fields...)
	conditions := make([]OperationalCondition, 0)
	if field.Malformed {
		return conditions, true
	}
	if !field.Found {
		return conditions, false
	}
	partial := false
	for index, raw := range field.Value {
		if index >= readPackFieldLimit {
			return conditions, true
		}
		condition, ok := raw.(map[string]interface{})
		if !ok {
			partial = true
			continue
		}
		typeField := readPackString(condition, "type")
		statusField := readPackString(condition, "status")
		reasonField := readPackString(condition, "reason")
		if typeField.Malformed || statusField.Malformed || reasonField.Malformed {
			partial = true
		}
		if typeField.Value == "" && statusField.Value == "" && reasonField.Value == "" {
			continue
		}
		conditions = append(conditions, OperationalCondition{Type: typeField.Value, Status: statusField.Value, Reason: reasonField.Value})
	}
	return conditions, partial
}

// readPackQuantityValue returns a safe quantity string without echoing malformed provider data.
func readPackQuantityValue(value interface{}) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if len(text) > readPackScalarLimit {
		return "", false
	}
	if text == "" {
		return "", true
	}
	if _, err := resource.ParseQuantity(text); err != nil {
		return "", false
	}
	return text, true
}

// readPackQuantityMap copies bounded valid Kubernetes quantities from one generic map.
func readPackQuantityMap(object map[string]interface{}, fields ...string) (map[string]string, bool, bool) {
	field := readPackMap(object, fields...)
	result := make(map[string]string)
	if field.Malformed || !field.Found {
		return result, field.Found, field.Malformed
	}
	partial := false
	keys := make([]string, 0, len(field.Value))
	for key := range field.Value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		if index >= readPackFieldLimit {
			partial = true
			break
		}
		if len(key) > readPackScalarLimit {
			partial = true
			continue
		}
		value, valid := readPackQuantityValue(field.Value[key])
		if !valid {
			partial = true
			continue
		}
		result[key] = value
	}
	return result, true, partial
}

// selectorSummary parses a native LabelSelector into a display-safe summary and list selector.
func selectorSummary(object map[string]interface{}, fields ...string) (OperationalLabelSelector, labels.Selector, bool, bool) {
	selector := OperationalLabelSelector{MatchLabels: map[string]string{}, MatchExpressions: []OperationalLabelExpression{}}
	field := readPackMap(object, fields...)
	if field.Malformed {
		return selector, labels.Nothing(), false, true
	}
	if !field.Found {
		return selector, labels.Everything(), true, false
	}
	parsed := metav1.LabelSelector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(field.Value, &parsed); err != nil {
		return selector, labels.Nothing(), false, true
	}
	if len(parsed.MatchLabels) > readPackResultLimit || len(parsed.MatchExpressions) > readPackResultLimit {
		return selector, labels.Nothing(), false, true
	}
	for key, value := range parsed.MatchLabels {
		if len(key) > readPackScalarLimit || len(value) > readPackScalarLimit || len(selector.MatchLabels) >= readPackResultLimit {
			return OperationalLabelSelector{MatchLabels: map[string]string{}, MatchExpressions: []OperationalLabelExpression{}}, labels.Nothing(), false, true
		}
		selector.MatchLabels[key] = value
	}
	for _, expression := range parsed.MatchExpressions {
		if len(expression.Key) > readPackScalarLimit || len(string(expression.Operator)) > readPackScalarLimit || len(expression.Values) > readPackFieldLimit || len(selector.MatchExpressions) >= readPackResultLimit {
			return OperationalLabelSelector{MatchLabels: map[string]string{}, MatchExpressions: []OperationalLabelExpression{}}, labels.Nothing(), false, true
		}
		for _, value := range expression.Values {
			if len(value) > readPackScalarLimit {
				return OperationalLabelSelector{MatchLabels: map[string]string{}, MatchExpressions: []OperationalLabelExpression{}}, labels.Nothing(), false, true
			}
		}
		selector.MatchExpressions = append(selector.MatchExpressions, OperationalLabelExpression{Key: expression.Key, Operator: string(expression.Operator), Values: append([]string(nil), expression.Values...)})
	}
	converted, err := metav1.LabelSelectorAsSelector(&parsed)
	if err != nil {
		return selector, labels.Nothing(), false, true
	}
	selector.Text = converted.String()
	if len(selector.Text) > readPackScalarLimit {
		return OperationalLabelSelector{MatchLabels: map[string]string{}, MatchExpressions: []OperationalLabelExpression{}}, labels.Nothing(), false, true
	}
	return selector, converted, true, false
}

// parseQuantityPercentage returns an integer percentage for comparable quota quantities.
func parseQuantityPercentage(used, hard string) *uint64 {
	usedQuantity, usedErr := resource.ParseQuantity(used)
	hardQuantity, hardErr := resource.ParseQuantity(hard)
	if usedErr != nil || hardErr != nil || hardQuantity.Sign() <= 0 {
		return nil
	}
	percentage := uint64(usedQuantity.AsApproximateFloat64() / hardQuantity.AsApproximateFloat64() * 100)
	return &percentage
}

// readPackBoolPointer copies a bool into an optional summary field.
func readPackBoolPointer(value bool) *bool {
	return &value
}

// int64Pointer copies an integer into an optional summary field.
func int64Pointer(value int64) *int64 {
	return &value
}

// parseNumericString returns a safe display string for an integer or quantity-like field.
func parseNumericString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
