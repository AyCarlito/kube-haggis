package kube

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
)

// TestBuildOperationalReadPackExtractsPodPostureAndRedactsSecretValues verifies the rich Pod panel contract.
func TestBuildOperationalReadPackExtractsPodPostureAndRedactsSecretValues(t *testing.T) {
	pod := relatedObject("v1", "Pod", "api", "default", "pod-uid", map[string]interface{}{
		"spec": map[string]interface{}{
			"nodeName":      "worker-a",
			"schedulerName": "default-scheduler",
			"securityContext": map[string]interface{}{
				"runAsNonRoot":   true,
				"seccompProfile": map[string]interface{}{"type": "RuntimeDefault"},
			},
			"containers": []interface{}{map[string]interface{}{
				"name":           "api",
				"image":          "example/api:1",
				"env":            []interface{}{map[string]interface{}{"name": "TOKEN", "value": "private-value"}},
				"readinessProbe": map[string]interface{}{"httpGet": map[string]interface{}{"path": "/ready"}},
			}},
			"volumes": []interface{}{map[string]interface{}{
				"name":   "credentials",
				"secret": map[string]interface{}{"secretName": "api-credentials"},
			}},
		},
		"status": map[string]interface{}{
			"phase":      "Running",
			"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}},
			"containerStatuses": []interface{}{map[string]interface{}{
				"name":         "api",
				"ready":        true,
				"restartCount": int64(2),
				"state": map[string]interface{}{
					"running": map[string]interface{}{},
				},
				"lastState": map[string]interface{}{
					"terminated": map[string]interface{}{"reason": "OOMKilled", "exitCode": int64(137)},
				},
			}},
		},
	})
	definition := readPackDefinition("", "v1", "pods", "Pod", true)
	pack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{definition}, pod), []ResourceDefinition{definition}, definition, pod, newRelatedCallBudget(context.Background()), time.Now())

	panel := pack.Panels[0]
	if panel.State != readPackStateAvailable {
		t.Fatalf("Pod panel was not available: %+v", panel)
	}
	data, ok := panel.Data.(*PodReadPackData)
	if !ok || len(data.Containers) != 1 || len(data.Containers[0].Probes) != 1 || data.Containers[0].LastTerminationReason != "OOMKilled" {
		t.Fatalf("Pod posture summary was incomplete: %#v", panel.Data)
	}
	encoded, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal Pod read pack: %v", err)
	}
	if strings.Contains(string(encoded), "private-value") {
		t.Fatalf("Pod read pack leaked an environment value: %s", encoded)
	}
}

// TestBuildOperationalReadPackReportsMissingEndpointSliceAPI verifies optional API absence is panel-local.
func TestBuildOperationalReadPackReportsMissingEndpointSliceAPI(t *testing.T) {
	ingress := relatedObject("networking.k8s.io/v1", "Ingress", "api", "default", "ingress-uid", map[string]interface{}{
		"spec": map[string]interface{}{
			"rules": []interface{}{map[string]interface{}{
				"host": "api.example.test",
				"http": map[string]interface{}{"paths": []interface{}{map[string]interface{}{
					"path":     "/",
					"pathType": "Prefix",
					"backend":  map[string]interface{}{"service": map[string]interface{}{"name": "api", "port": map[string]interface{}{"number": int64(80)}}},
				}}},
			}},
		},
	})
	service := relatedObject("v1", "Service", "api", "default", "service-uid", nil)
	definitions := []ResourceDefinition{
		readPackDefinition("networking.k8s.io", "v1", "ingresses", "Ingress", true),
		readPackDefinition("", "v1", "services", "Service", true),
	}
	client := newRelatedDynamicClientForDefinitions(definitions, ingress, service)
	pack := buildOperationalReadPack(context.Background(), client, definitions, definitions[0], ingress, newRelatedCallBudget(context.Background()), time.Now())
	panel := pack.Panels[0]
	if panel.State != readPackStatePartial || !strings.Contains(panel.Message, "not present in discovery") {
		t.Fatalf("missing EndpointSlice API was not explicit: %+v", panel)
	}
	data, ok := panel.Data.(*IngressReadPackData)
	if !ok || len(data.Backends) != 1 || len(data.Services) != 1 || data.Services[0].State != "resolved" {
		t.Fatalf("Ingress backend data was hidden by optional API absence: %#v", panel.Data)
	}
}

// TestBuildOperationalReadPackReportsEmptyAndMalformedEndpointSlices verifies explicit empty and partial states.
func TestBuildOperationalReadPackReportsEmptyAndMalformedEndpointSlices(t *testing.T) {
	definition := readPackDefinition("discovery.k8s.io", "v1", "endpointslices", "EndpointSlice", true)
	tests := []struct {
		name      string
		object    *unstructured.Unstructured
		wantState string
	}{
		{
			name:      "empty",
			object:    relatedObject("discovery.k8s.io/v1", "EndpointSlice", "api-1", "default", "slice-uid", map[string]interface{}{"addressType": "IPv4"}),
			wantState: readPackStateEmpty,
		},
		{
			name: "malformed",
			object: relatedObject("discovery.k8s.io/v1", "EndpointSlice", "api-1", "default", "slice-uid", map[string]interface{}{
				"ports": []interface{}{map[string]interface{}{"port": map[string]interface{}{"unexpected": true}}},
			}),
			wantState: readPackStatePartial,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{definition}, test.object), []ResourceDefinition{definition}, definition, test.object, newRelatedCallBudget(context.Background()), time.Now())
			if pack.Panels[0].State != test.wantState {
				t.Fatalf("unexpected EndpointSlice state: got %s want %s panel=%+v", pack.Panels[0].State, test.wantState, pack.Panels[0])
			}
			if test.name == "malformed" && !strings.Contains(pack.Panels[0].Message, "provider-specific") {
				t.Fatalf("malformed field state did not retain safe explanation: %+v", pack.Panels[0])
			}
		})
	}
}

// TestBuildOperationalReadPackReportsRBACDeniedServiceAccountBindings verifies permission state is safe and local.
func TestBuildOperationalReadPackReportsRBACDeniedServiceAccountBindings(t *testing.T) {
	serviceAccount := relatedObject("v1", "ServiceAccount", "deployer", "default", "sa-uid", map[string]interface{}{
		"secrets":          []interface{}{map[string]interface{}{"name": "deployer-token"}},
		"imagePullSecrets": []interface{}{map[string]interface{}{"name": "registry"}},
	})
	definitions := []ResourceDefinition{
		readPackDefinition("", "v1", "serviceaccounts", "ServiceAccount", true),
		readPackDefinition("rbac.authorization.k8s.io", "v1", "rolebindings", "RoleBinding", true),
		readPackDefinition("rbac.authorization.k8s.io", "v1", "clusterrolebindings", "ClusterRoleBinding", false),
	}
	client := newRelatedDynamicClientForDefinitions(definitions, serviceAccount)
	client.PrependReactor("list", "rolebindings", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "rolebindings"}, "rolebindings", nil)
	})
	pack := buildOperationalReadPack(context.Background(), client, definitions, definitions[0], serviceAccount, newRelatedCallBudget(context.Background()), time.Now())
	panel := pack.Panels[0]
	if panel.State != readPackStatePartial || !strings.Contains(panel.Message, "RBAC denied") {
		t.Fatalf("RBAC denial was not visible in the ServiceAccount panel: %+v", panel)
	}
	encoded, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal ServiceAccount read pack: %v", err)
	}
	if strings.Contains(string(encoded), "token-value") {
		t.Fatalf("ServiceAccount read pack leaked a Secret value: %s", encoded)
	}
}

// TestBuildOperationalReadPackCapsAffectedPods verifies the fixed result cap on selector expansion.
func TestBuildOperationalReadPackCapsAffectedPods(t *testing.T) {
	policy := relatedObject("networking.k8s.io/v1", "NetworkPolicy", "api", "default", "policy-uid", map[string]interface{}{
		"spec": map[string]interface{}{
			"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "api"}},
		},
	})
	objects := []runtime.Object{policy}
	for index := 0; index < readPackResultLimit+20; index++ {
		objects = append(objects, relatedObjectWithLabelsAndOwners("v1", "Pod", "api-"+strconv.Itoa(index), "default", "pod-"+strconv.Itoa(index), map[string]string{"app": "api"}, nil))
	}
	definition := readPackDefinition("networking.k8s.io", "v1", "networkpolicies", "NetworkPolicy", true)
	podDefinition := readPackDefinition("", "v1", "pods", "Pod", true)
	definitions := []ResourceDefinition{definition, podDefinition}
	client := newRelatedDynamicClientForDefinitions(definitions, objects...)
	pack := buildOperationalReadPack(context.Background(), client, definitions, definition, policy, newRelatedCallBudget(context.Background()), time.Now())
	data := pack.Panels[0].Data.(*NetworkPolicyReadPackData)
	if len(data.AffectedPods) > readPackResultLimit || pack.Panels[0].State != readPackStatePartial || !pack.Panels[0].Truncated && len(data.AffectedPods) != readPackResultLimit || pack.Diagnostics.QueryCount > relatedCallBudgetLimit {
		t.Fatalf("NetworkPolicy Pod expansion was not capped: state=%s truncated=%t count=%d queries=%d", pack.Panels[0].State, pack.Panels[0].Truncated, len(data.AffectedPods), pack.Diagnostics.QueryCount)
	}
}

// TestBuildOperationalReadPackCapsIngressCollections verifies nested rules, TLS hosts, and backends share a hard response cap.
func TestBuildOperationalReadPackCapsIngressCollections(t *testing.T) {
	rules := make([]interface{}, 0, 4)
	for ruleIndex := 0; ruleIndex < 4; ruleIndex++ {
		paths := make([]interface{}, 0, readPackRuleLimit)
		for pathIndex := 0; pathIndex < readPackRuleLimit; pathIndex++ {
			paths = append(paths, map[string]interface{}{
				"path":    "/" + strconv.Itoa(pathIndex),
				"backend": map[string]interface{}{"service": map[string]interface{}{"name": "service-" + strconv.Itoa(ruleIndex) + "-" + strconv.Itoa(pathIndex)}},
			})
		}
		rules = append(rules, map[string]interface{}{"host": "host-" + strconv.Itoa(ruleIndex) + ".example.test", "http": map[string]interface{}{"paths": paths}})
	}
	tls := make([]interface{}, 0, 4)
	for index := 0; index < 4; index++ {
		hosts := make([]interface{}, 0, readPackFieldLimit)
		for hostIndex := 0; hostIndex < readPackFieldLimit; hostIndex++ {
			hosts = append(hosts, "tls-"+strconv.Itoa(index)+"-"+strconv.Itoa(hostIndex)+".example.test")
		}
		tls = append(tls, map[string]interface{}{"hosts": hosts})
	}
	definition := readPackDefinition("networking.k8s.io", "v1", "ingresses", "Ingress", true)
	object := relatedObject("networking.k8s.io/v1", "Ingress", "api", "default", "ingress-uid", map[string]interface{}{"spec": map[string]interface{}{"rules": rules, "tls": tls}})
	pack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{definition}, object), []ResourceDefinition{definition}, definition, object, newRelatedCallBudget(context.Background()), time.Now())
	data := pack.Panels[0].Data.(*IngressReadPackData)
	pathCount := 0
	for _, rule := range data.Rules {
		pathCount += len(rule.Paths)
	}
	aggregateCount := len(data.Rules) + pathCount + len(data.Backends) + len(data.TLSHosts)
	if aggregateCount > readPackResultLimit || !pack.Panels[0].Truncated || pack.Panels[0].State != readPackStatePartial {
		t.Fatalf("Ingress nested collections exceeded or hid the aggregate cap: state=%s truncated=%t rules=%d paths=%d backends=%d tls=%d aggregate=%d", pack.Panels[0].State, pack.Panels[0].Truncated, len(data.Rules), pathCount, len(data.Backends), len(data.TLSHosts), aggregateCount)
	}
}

// TestBuildOperationalReadPackCapsIngressDependencies verifies Service and EndpointSlice output shares the Ingress budget.
func TestBuildOperationalReadPackCapsIngressDependencies(t *testing.T) {
	ingressDefinition := readPackDefinition("networking.k8s.io", "v1", "ingresses", "Ingress", true)
	serviceDefinition := readPackDefinition("", "v1", "services", "Service", true)
	endpointDefinition := readPackDefinition("discovery.k8s.io", "v1", "endpointslices", "EndpointSlice", true)
	ingress := relatedObject("networking.k8s.io/v1", "Ingress", "api", "default", "ingress-uid", map[string]interface{}{
		"spec": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"http": map[string]interface{}{"paths": []interface{}{map[string]interface{}{"path": "/", "backend": map[string]interface{}{"service": map[string]interface{}{"name": "api"}}}}}}}},
	})
	service := relatedObject("v1", "Service", "api", "default", "service-uid", nil)
	endpointSlice := relatedObject("discovery.k8s.io/v1", "EndpointSlice", "api-1", "default", "slice-uid", map[string]interface{}{
		"addressType": "IPv4",
		"ports":       []interface{}{map[string]interface{}{"name": "http", "protocol": "TCP", "port": int64(80)}, map[string]interface{}{"name": "https", "protocol": "TCP", "port": int64(443)}},
	})
	endpointSlice.SetLabels(map[string]string{"kubernetes.io/service-name": "api"})
	endpoints := make([]interface{}, 0, readPackResultLimit+20)
	for index := 0; index < readPackResultLimit+20; index++ {
		endpoints = append(endpoints, map[string]interface{}{"addresses": []interface{}{"10.0.0." + strconv.Itoa(index+1)}, "conditions": map[string]interface{}{"ready": true}})
	}
	endpointSlice.Object["endpoints"] = endpoints
	definitions := []ResourceDefinition{ingressDefinition, serviceDefinition, endpointDefinition}
	objects := []runtime.Object{ingress, service, endpointSlice}
	client := newRelatedDynamicClientForDefinitions(definitions, objects...)
	pack := buildOperationalReadPack(context.Background(), client, definitions, ingressDefinition, ingress, newRelatedCallBudget(context.Background()), time.Now())
	data := pack.Panels[0].Data.(*IngressReadPackData)
	aggregateCount := len(data.Rules) + len(data.Backends) + len(data.TLSHosts) + len(data.Services)
	for _, rule := range data.Rules {
		aggregateCount += len(rule.Paths)
	}
	for _, slice := range data.EndpointSlices {
		aggregateCount++
		aggregateCount += len(slice.Ports) + len(slice.Endpoints)
		for _, endpoint := range slice.Endpoints {
			aggregateCount += len(endpoint.Addresses)
		}
	}
	encoded, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal Ingress dependency read pack: %v", err)
	}
	if aggregateCount > readPackResultLimit || !pack.Panels[0].Truncated || pack.Panels[0].State != readPackStatePartial || len(data.Services) != 1 || len(data.EndpointSlices) != 1 || len(data.EndpointSlices[0].Endpoints) == 0 {
		t.Fatalf("Ingress dependency output exceeded or hid aggregate cap: state=%s truncated=%t aggregate=%d services=%d slices=%d endpoints=%d body-size=%d", pack.Panels[0].State, pack.Panels[0].Truncated, aggregateCount, len(data.Services), len(data.EndpointSlices), len(data.EndpointSlices[0].Endpoints), len(encoded))
	}
}

// TestBuildOperationalReadPackCapsNetworkPolicyNestedCollections verifies peers and ports share one panel budget.
func TestBuildOperationalReadPackCapsNetworkPolicyNestedCollections(t *testing.T) {
	rules := make([]interface{}, 0, 4)
	for ruleIndex := 0; ruleIndex < 4; ruleIndex++ {
		peers := make([]interface{}, 0, readPackFieldLimit)
		ports := make([]interface{}, 0, readPackFieldLimit)
		for itemIndex := 0; itemIndex < readPackFieldLimit; itemIndex++ {
			peers = append(peers, map[string]interface{}{"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "api"}}})
			ports = append(ports, map[string]interface{}{"protocol": "TCP", "port": int64(8000 + itemIndex)})
		}
		rules = append(rules, map[string]interface{}{"from": peers, "ports": ports})
		_ = ruleIndex
	}
	definition := readPackDefinition("networking.k8s.io", "v1", "networkpolicies", "NetworkPolicy", true)
	object := relatedObject("networking.k8s.io/v1", "NetworkPolicy", "api", "default", "policy-uid", map[string]interface{}{"spec": map[string]interface{}{"policyTypes": []interface{}{"Ingress", "Egress"}, "podSelector": map[string]interface{}{}, "ingress": rules}})
	pack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{definition}, object), []ResourceDefinition{definition}, definition, object, newRelatedCallBudget(context.Background()), time.Now())
	data := pack.Panels[0].Data.(*NetworkPolicyReadPackData)
	peerCount := 0
	portCount := 0
	for _, rule := range data.IngressRules {
		peerCount += len(rule.Peers)
		portCount += len(rule.Ports)
	}
	aggregateCount := len(data.PolicyTypes) + len(data.IngressRules) + peerCount + portCount + len(data.AffectedPods)
	if aggregateCount > readPackResultLimit || !pack.Panels[0].Truncated || pack.Panels[0].State != readPackStatePartial {
		t.Fatalf("NetworkPolicy nested collections exceeded or hid the aggregate cap: state=%s truncated=%t rules=%d peers=%d ports=%d affected=%d aggregate=%d", pack.Panels[0].State, pack.Panels[0].Truncated, len(data.IngressRules), peerCount, portCount, len(data.AffectedPods), aggregateCount)
	}
}

// TestBuildOperationalReadPackBoundsOversizedScalars verifies provider strings and quantity displays are omitted safely.
func TestBuildOperationalReadPackBoundsOversizedScalars(t *testing.T) {
	giant := strings.Repeat("provider-secret-value-", readPackScalarLimit)
	podDefinition := readPackDefinition("", "v1", "pods", "Pod", true)
	pod := relatedObject("v1", "Pod", "api", "default", "pod-uid", map[string]interface{}{
		"spec":   map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "api", "image": giant, "env": []interface{}{map[string]interface{}{"name": "TOKEN", "value": giant}}}}},
		"status": map[string]interface{}{"reason": giant},
	})
	podPack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{podDefinition}, pod), []ResourceDefinition{podDefinition}, podDefinition, pod, newRelatedCallBudget(context.Background()), time.Now())
	podJSON, err := json.Marshal(podPack)
	if err != nil {
		t.Fatalf("marshal oversized Pod read pack: %v", err)
	}
	if podPack.Panels[0].State != readPackStatePartial || strings.Contains(string(podJSON), giant) || strings.Contains(string(podJSON), "provider-secret-value") {
		t.Fatalf("oversized Pod scalar or Secret value was returned: state=%s body-size=%d", podPack.Panels[0].State, len(podJSON))
	}

	quotaDefinition := readPackDefinition("", "v1", "resourcequotas", "ResourceQuota", true)
	quota := relatedObject("v1", "ResourceQuota", "team", "default", "quota-uid", map[string]interface{}{"spec": map[string]interface{}{"hard": map[string]interface{}{"pods": giant}}})
	quotaPack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{quotaDefinition}, quota), []ResourceDefinition{quotaDefinition}, quotaDefinition, quota, newRelatedCallBudget(context.Background()), time.Now())
	quotaJSON, err := json.Marshal(quotaPack)
	if err != nil {
		t.Fatalf("marshal oversized quota read pack: %v", err)
	}
	if quotaPack.Panels[0].State != readPackStatePartial || strings.Contains(string(quotaJSON), giant) {
		t.Fatalf("oversized quantity was returned without a partial state: state=%s body=%s", quotaPack.Panels[0].State, quotaJSON)
	}

	policyDefinition := readPackDefinition("networking.k8s.io", "v1", "networkpolicies", "NetworkPolicy", true)
	policy := relatedObject("networking.k8s.io/v1", "NetworkPolicy", "api", "default", "policy-uid", map[string]interface{}{"spec": map[string]interface{}{"podSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": giant}}}})
	policyPack := buildOperationalReadPack(context.Background(), newRelatedDynamicClientForDefinitions([]ResourceDefinition{policyDefinition}, policy), []ResourceDefinition{policyDefinition}, policyDefinition, policy, newRelatedCallBudget(context.Background()), time.Now())
	policyJSON, err := json.Marshal(policyPack)
	if err != nil {
		t.Fatalf("marshal oversized selector read pack: %v", err)
	}
	if policyPack.Panels[0].State != readPackStatePartial || strings.Contains(string(policyJSON), giant) {
		t.Fatalf("oversized selector was returned without a partial state: state=%s body=%s", policyPack.Panels[0].State, policyJSON)
	}
}

// TestOperationalReadPackExtractsStatusAndQuotaFamilies verifies representative scalar panels remain structured.
func TestOperationalReadPackExtractsStatusAndQuotaFamilies(t *testing.T) {
	tests := []struct {
		name       string
		definition ResourceDefinition
		object     *unstructured.Unstructured
		wantState  string
	}{
		{
			name:       "HPA",
			definition: readPackDefinition("autoscaling", "v2", "horizontalpodautoscalers", "HorizontalPodAutoscaler", true),
			object: relatedObject("autoscaling/v2", "HorizontalPodAutoscaler", "api", "default", "hpa-uid", map[string]interface{}{
				"spec":   map[string]interface{}{"scaleTargetRef": map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "name": "api"}, "minReplicas": int64(2), "maxReplicas": int64(5)},
				"status": map[string]interface{}{"currentReplicas": int64(2), "desiredReplicas": int64(3), "conditions": []interface{}{map[string]interface{}{"type": "AbleToScale", "status": "True"}}},
			}),
			wantState: readPackStateAvailable,
		},
		{
			name:       "PDB",
			definition: readPackDefinition("policy", "v1", "poddisruptionbudgets", "PodDisruptionBudget", true),
			object: relatedObject("policy/v1", "PodDisruptionBudget", "api", "default", "pdb-uid", map[string]interface{}{
				"spec":   map[string]interface{}{"minAvailable": "1"},
				"status": map[string]interface{}{"expectedPods": int64(3), "currentHealthy": int64(3), "desiredHealthy": int64(1), "disruptionsAllowed": int64(2)},
			}),
			wantState: readPackStateAvailable,
		},
		{
			name:       "ResourceQuota",
			definition: readPackDefinition("", "v1", "resourcequotas", "ResourceQuota", true),
			object: relatedObject("v1", "ResourceQuota", "team", "default", "quota-uid", map[string]interface{}{
				"spec":   map[string]interface{}{"hard": map[string]interface{}{"pods": "10"}},
				"status": map[string]interface{}{"used": map[string]interface{}{"pods": "4"}},
			}),
			wantState: readPackStateAvailable,
		},
		{
			name:       "LimitRange",
			definition: readPackDefinition("", "v1", "limitranges", "LimitRange", true),
			object: relatedObject("v1", "LimitRange", "defaults", "default", "limits-uid", map[string]interface{}{
				"spec": map[string]interface{}{"limits": []interface{}{map[string]interface{}{"type": "Container", "default": map[string]interface{}{"cpu": "1"}}}},
			}),
			wantState: readPackStateAvailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newRelatedDynamicClientForDefinitions([]ResourceDefinition{test.definition}, test.object)
			pack := buildOperationalReadPack(context.Background(), client, []ResourceDefinition{test.definition}, test.definition, test.object, newRelatedCallBudget(context.Background()), time.Now())
			if pack.Panels[0].State != test.wantState {
				t.Fatalf("unexpected %s read-pack state: %+v", test.name, pack.Panels[0])
			}
		})
	}
}

// TestReadPackDefinitionKeepsExactGVRIdentity verifies optional family checks do not resolve short names.
func TestReadPackDefinitionKeepsExactGVRIdentity(t *testing.T) {
	definitions := []ResourceDefinition{readPackDefinition("apps", "v1", "deployments", "Deployment", true)}
	if _, found := exactResourceDefinition(definitions, "", "v1", "deployments"); found {
		t.Fatal("read-pack definition lookup resolved a short or wrong-group identity")
	}
}

// readPackDefinition creates a deterministic discovery definition for read-pack tests.
func readPackDefinition(group, version, resource, kind string, namespaced bool) ResourceDefinition {
	return ResourceDefinition{Group: group, Version: version, Resource: resource, Kind: kind, Namespaced: namespaced, Scope: resourceScope(namespaced), Verbs: []string{"get", "list"}}
}
