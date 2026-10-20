// Package kube provides discovery-driven access to Kubernetes clusters.
package kube

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

// ContextSummary describes a kubeconfig context in a form suitable for the UI.
type ContextSummary struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Server    string `json:"server"`
	IsCurrent bool   `json:"isCurrent"`
	Reachable bool   `json:"reachable"`
	LastError string `json:"lastError,omitempty"`
}

// ResourceDefinition describes a discovered Kubernetes resource kind.
type ResourceDefinition struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	ShortNames []string `json:"shortNames,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// ResourceSummary contains the common fields shown in resource tables.
type ResourceSummary struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	Kind            string            `json:"kind"`
	UID             string            `json:"uid"`
	CreatedAt       time.Time         `json:"createdAt"`
	Status          string            `json:"status"`
	Ready           string            `json:"ready,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	ResourceVersion string            `json:"resourceVersion"`
}

// ResourceList is the generic response returned for a resource collection.
type ResourceList struct {
	Definition      ResourceDefinition `json:"definition"`
	Items           []ResourceSummary  `json:"items"`
	Continue        string             `json:"continue,omitempty"`
	ResourceVersion string             `json:"resourceVersion"`
}

// NodeOverview summarizes node readiness and scheduling state.
type NodeOverview struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	NotReady int `json:"notReady"`
	Cordoned int `json:"cordoned"`
	Pressure int `json:"pressure"`
}

// WorkloadOverview summarizes the workload families used by the dashboard.
type WorkloadOverview struct {
	Deployments         int `json:"deployments"`
	HealthyDeployments  int `json:"healthyDeployments"`
	StatefulSets        int `json:"statefulSets"`
	HealthyStatefulSets int `json:"healthyStatefulSets"`
	DaemonSets          int `json:"daemonSets"`
	HealthyDaemonSets   int `json:"healthyDaemonSets"`
	Jobs                int `json:"jobs"`
	FailedJobs          int `json:"failedJobs"`
	CronJobs            int `json:"cronJobs"`
}

// PodOverview summarizes Pod phase and restart health.
type PodOverview struct {
	Total      int `json:"total"`
	Running    int `json:"running"`
	Pending    int `json:"pending"`
	Failed     int `json:"failed"`
	Restarting int `json:"restarting"`
}

// StorageOverview summarizes PersistentVolumeClaim phases.
type StorageOverview struct {
	Total   int `json:"total"`
	Bound   int `json:"bound"`
	Pending int `json:"pending"`
	Lost    int `json:"lost"`
}

// EventSummary contains the useful, compact portion of a Kubernetes Event.
type EventSummary struct {
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Namespace string    `json:"namespace,omitempty"`
	Object    string    `json:"object"`
	LastSeen  time.Time `json:"lastSeen"`
}

// ClusterOverview is the aggregate model used by the cluster landing page.
type ClusterOverview struct {
	Context          ContextSummary   `json:"context"`
	Namespaces       int              `json:"namespaces"`
	Nodes            NodeOverview     `json:"nodes"`
	Workloads        WorkloadOverview `json:"workloads"`
	Pods             PodOverview      `json:"pods"`
	Storage          StorageOverview  `json:"storage"`
	Warnings         []EventSummary   `json:"warnings"`
	MetricsAvailable bool             `json:"metricsAvailable"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

// Service owns kubeconfig loading and creates dynamic clients for contexts.
type Service struct {
	rules *clientcmd.ClientConfigLoadingRules
}

// NewService creates a Kubernetes service using standard kubeconfig rules.
func NewService(kubeconfigPath string) *Service {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if strings.TrimSpace(kubeconfigPath) != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	return &Service{rules: rules}
}

// Contexts returns all contexts and the kubeconfig current-context value.
func (s *Service) Contexts() ([]ContextSummary, string, error) {
	config, err := s.loadConfig()
	if err != nil {
		return nil, "", err
	}

	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	contexts := make([]ContextSummary, 0, len(names))
	for _, name := range names {
		contexts = append(contexts, summarizeContext(config, name))
	}
	return contexts, config.CurrentContext, nil
}

// Discovery returns the preferred resource definitions advertised by a cluster.
func (s *Service) Discovery(ctx context.Context, contextName string) ([]ResourceDefinition, error) {
	_, discoveryClient, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}

	resourceLists, discoveryErr := discoveryClient.ServerPreferredResources()
	if discoveryErr != nil && len(resourceLists) == 0 {
		return nil, discoveryErr
	}

	definitions := make([]ResourceDefinition, 0)
	for _, resourceList := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range resourceList.APIResources {
			definitions = append(definitions, ResourceDefinition{
				Group:      groupVersion.Group,
				Version:    groupVersion.Version,
				Resource:   resource.Name,
				Kind:       resource.Kind,
				Namespaced: resource.Namespaced,
				Verbs:      append([]string(nil), resource.Verbs...),
				ShortNames: append([]string(nil), resource.ShortNames...),
				Categories: append([]string(nil), resource.Categories...),
			})
		}
	}

	sort.Slice(definitions, func(i, j int) bool {
		left := definitions[i].Group + "/" + definitions[i].Version + "/" + definitions[i].Resource
		right := definitions[j].Group + "/" + definitions[j].Version + "/" + definitions[j].Resource
		return left < right
	})
	return definitions, nil
}

// Overview gathers the data needed by the cluster landing page.
func (s *Service) Overview(ctx context.Context, contextName string) (ClusterOverview, error) {
	contexts, currentContext, err := s.Contexts()
	if err != nil {
		return ClusterOverview{}, err
	}
	if strings.TrimSpace(contextName) == "" {
		contextName = currentContext
	}
	contextSummary, found := findContext(contexts, contextName)
	if !found {
		return ClusterOverview{}, fmt.Errorf("context %q was not found", contextName)
	}

	dynamicClient, discoveryClient, _, err := s.clients(contextName)
	if err != nil {
		return ClusterOverview{}, err
	}

	nodes, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("nodes"))
	if err != nil {
		return ClusterOverview{}, err
	}
	namespaces, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("namespaces"))
	if err != nil {
		return ClusterOverview{}, err
	}
	pods, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("pods"))
	if err != nil {
		return ClusterOverview{}, err
	}
	deployments, err := s.listOverviewResource(ctx, dynamicClient, appsGVR("deployments"))
	if err != nil {
		return ClusterOverview{}, err
	}
	statefulSets, err := s.listOverviewResource(ctx, dynamicClient, appsGVR("statefulsets"))
	if err != nil {
		return ClusterOverview{}, err
	}
	daemonSets, err := s.listOverviewResource(ctx, dynamicClient, appsGVR("daemonsets"))
	if err != nil {
		return ClusterOverview{}, err
	}
	jobs, err := s.listOverviewResource(ctx, dynamicClient, batchGVR("jobs"))
	if err != nil {
		return ClusterOverview{}, err
	}
	cronJobs, err := s.listOverviewResource(ctx, dynamicClient, batchGVR("cronjobs"))
	if err != nil {
		return ClusterOverview{}, err
	}
	claims, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("persistentvolumeclaims"))
	if err != nil {
		return ClusterOverview{}, err
	}
	events, err := s.listOverviewResource(ctx, dynamicClient, coreGVR("events"))
	if err != nil {
		return ClusterOverview{}, err
	}

	metricsAvailable := false
	if _, metricsErr := discoveryClient.ServerResourcesForGroupVersion("metrics.k8s.io/v1beta1"); metricsErr == nil {
		metricsAvailable = true
	}

	return ClusterOverview{
		Context:          contextSummary,
		Namespaces:       len(namespaces.Items),
		Nodes:            buildNodeOverview(nodes.Items),
		Workloads:        buildWorkloadOverview(deployments.Items, statefulSets.Items, daemonSets.Items, jobs.Items, cronJobs.Items),
		Pods:             buildPodOverview(pods.Items),
		Storage:          buildStorageOverview(claims.Items),
		Warnings:         buildWarningEvents(events.Items),
		MetricsAvailable: metricsAvailable,
		UpdatedAt:        time.Now().UTC(),
	}, nil
}

// ListResources lists any discovered resource using a group-version-resource reference.
func (s *Service) ListResources(ctx context.Context, contextName, group, version, resource, namespace string, namespaced bool, limit int, labelSelector string) (ResourceList, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return ResourceList{}, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	options := metav1.ListOptions{LabelSelector: labelSelector}
	if limit > 0 {
		options.Limit = int64(limit)
	}
	items, err := listResource(ctx, dynamicClient, gvr, namespace, namespaced, options)
	if err != nil {
		return ResourceList{}, err
	}

	definition := ResourceDefinition{Group: group, Version: version, Resource: resource, Namespaced: namespaced}
	if definitions, discoveryErr := s.Discovery(ctx, contextName); discoveryErr == nil {
		for _, candidate := range definitions {
			if candidate.Group == group && candidate.Version == version && candidate.Resource == resource {
				definition = candidate
				break
			}
		}
	}

	result := ResourceList{
		Definition:      definition,
		Items:           make([]ResourceSummary, 0, len(items.Items)),
		Continue:        items.GetContinue(),
		ResourceVersion: items.GetResourceVersion(),
	}
	for index := range items.Items {
		result.Items = append(result.Items, summarizeResource(&items.Items[index]))
	}
	return result, nil
}

// WatchResources opens a Kubernetes watch for a discovered resource.
func (s *Service) WatchResources(ctx context.Context, contextName, group, version, resource, namespace string, namespaced bool, resourceVersion string) (watch.Interface, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	options := metav1.ListOptions{ResourceVersion: resourceVersion, AllowWatchBookmarks: true}
	return watchResource(ctx, dynamicClient, gvr, namespace, namespaced, options)
}

// GetResource returns the complete unstructured object for a resource name.
func (s *Service) GetResource(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced bool) (*unstructured.Unstructured, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
	resourceClient, err := resourceInterface(dynamicClient, gvr, namespace, namespaced, false)
	if err != nil {
		return nil, err
	}
	return resourceClient.Get(ctx, name, metav1.GetOptions{})
}

// ApplyResource creates or client-side updates a YAML or JSON resource document.
func (s *Service) ApplyResource(ctx context.Context, contextName, group, version, resource, namespace string, namespaced, dryRun bool, body []byte) (*unstructured.Unstructured, error) {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	var object map[string]interface{}
	if err := yaml.Unmarshal(body, &object); err != nil {
		return nil, fmt.Errorf("decode resource document: %w", err)
	}
	unstructured.RemoveNestedField(object, "metadata", "managedFields")
	unstructuredObject := &unstructured.Unstructured{Object: object}
	if namespace == "" || namespace == "all" {
		namespace = unstructuredObject.GetNamespace()
	}
	if namespaced && namespace == "" {
		return nil, fmt.Errorf("namespace is required for namespaced resources")
	}
	resourceClient, err := resourceInterface(dynamicClient, schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, namespace, namespaced, true)
	if err != nil {
		return nil, err
	}
	dryRunValues := []string(nil)
	if dryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	name := unstructuredObject.GetName()
	if name == "" {
		return resourceClient.Create(ctx, unstructuredObject, metav1.CreateOptions{DryRun: dryRunValues, FieldManager: "kube-haggis"})
	}
	if unstructuredObject.GetResourceVersion() == "" {
		current, getErr := resourceClient.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return nil, fmt.Errorf("read current resource version: %w", getErr)
		}
		unstructuredObject.SetResourceVersion(current.GetResourceVersion())
	}
	return resourceClient.Update(ctx, unstructuredObject, metav1.UpdateOptions{DryRun: dryRunValues, FieldManager: "kube-haggis"})
}

// DeleteResource deletes a resource with optional dry-run and force behavior.
func (s *Service) DeleteResource(ctx context.Context, contextName, group, version, resource, namespace, name string, namespaced, dryRun, force bool) error {
	dynamicClient, _, _, err := s.clients(contextName)
	if err != nil {
		return err
	}
	resourceClient, err := resourceInterface(dynamicClient, schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, namespace, namespaced, false)
	if err != nil {
		return err
	}
	dryRunValues := []string(nil)
	if dryRun {
		dryRunValues = []string{metav1.DryRunAll}
	}
	options := metav1.DeleteOptions{DryRun: dryRunValues}
	if force {
		gracePeriod := int64(0)
		options.GracePeriodSeconds = &gracePeriod
	}
	return resourceClient.Delete(ctx, name, options)
}

// PodLogs opens a native Kubernetes Pod log stream for one container.
func (s *Service) PodLogs(ctx context.Context, contextName, namespace, pod, container string, follow, previous bool, tailLines *int64) (io.ReadCloser, error) {
	_, _, restConfig, err := s.clients(contextName)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create typed Kubernetes client: %w", err)
	}
	options := &corev1.PodLogOptions{
		Container:  container,
		Follow:     follow,
		Previous:   previous,
		TailLines:  tailLines,
		Timestamps: true,
	}
	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(pod, options).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("open logs for Pod %q: %w", pod, err)
	}
	return stream, nil
}

// loadConfig loads the merged kubeconfig from the configured loading rules.
func (s *Service) loadConfig() (*clientcmdapi.Config, error) {
	config, err := s.rules.Load()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return config, nil
}

// clients creates dynamic and discovery clients for one kubeconfig context.
func (s *Service) clients(contextName string) (dynamic.Interface, discovery.DiscoveryInterface, *rest.Config, error) {
	config, err := s.loadConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(contextName) == "" {
		contextName = config.CurrentContext
	}
	if contextName == "" {
		return nil, nil, nil, fmt.Errorf("kubeconfig has no current context")
	}
	if _, found := config.Contexts[contextName]; !found {
		return nil, nil, nil, fmt.Errorf("context %q was not found", contextName)
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(s.rules, overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build client config for %q: %w", contextName, err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create discovery client: %w", err)
	}
	return dynamicClient, discoveryClient, restConfig, nil
}

// summarizeContext converts raw kubeconfig data into a UI-safe context summary.
func summarizeContext(config *clientcmdapi.Config, name string) ContextSummary {
	contextEntry := config.Contexts[name]
	if contextEntry == nil {
		return ContextSummary{Name: name, IsCurrent: config.CurrentContext == name}
	}
	cluster := config.Clusters[contextEntry.Cluster]
	server := ""
	if cluster != nil {
		server = cluster.Server
	}
	return ContextSummary{
		Name:      name,
		Cluster:   contextEntry.Cluster,
		User:      contextEntry.AuthInfo,
		Server:    server,
		IsCurrent: config.CurrentContext == name,
	}
}

// findContext finds a context summary by kubeconfig context name.
func findContext(contexts []ContextSummary, name string) (ContextSummary, bool) {
	for _, item := range contexts {
		if item.Name == name {
			return item, true
		}
	}
	return ContextSummary{}, false
}

// coreGVR builds a GroupVersionResource for a core API resource.
func coreGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Version: "v1", Resource: resource}
}

// appsGVR builds a GroupVersionResource for an apps API resource.
func appsGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resource}
}

// batchGVR builds a GroupVersionResource for a batch API resource.
func batchGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: resource}
}

// listOverviewResource lists an all-namespace resource for dashboard aggregation.
func (s *Service) listOverviewResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	items, err := client.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil && apierrors.IsNotFound(err) {
		return &unstructured.UnstructuredList{}, nil
	}
	return items, err
}

// listResource lists a namespaced or cluster-scoped resource according to discovery.
func listResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced bool, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if namespaced && strings.TrimSpace(namespace) != "" && namespace != "all" {
		return client.Resource(gvr).Namespace(namespace).List(ctx, options)
	}
	return client.Resource(gvr).List(ctx, options)
}

// watchResource opens a namespaced or cluster-scoped watch.
func watchResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced bool, options metav1.ListOptions) (watch.Interface, error) {
	if namespaced && strings.TrimSpace(namespace) != "" && namespace != "all" {
		return client.Resource(gvr).Namespace(namespace).Watch(ctx, options)
	}
	return client.Resource(gvr).Watch(ctx, options)
}

// resourceInterface returns the dynamic resource client for a mutation or get.
func resourceInterface(client dynamic.Interface, gvr schema.GroupVersionResource, namespace string, namespaced, requireNamespace bool) (dynamic.ResourceInterface, error) {
	if namespaced && requireNamespace && (strings.TrimSpace(namespace) == "" || namespace == "all") {
		return nil, fmt.Errorf("namespace is required for namespaced resources")
	}
	if namespaced && strings.TrimSpace(namespace) != "" && namespace != "all" {
		return client.Resource(gvr).Namespace(namespace), nil
	}
	return client.Resource(gvr), nil
}

// summarizeResource extracts common fields from an unstructured resource.
func summarizeResource(item *unstructured.Unstructured) ResourceSummary {
	return ResourceSummary{
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		Kind:            item.GetKind(),
		UID:             string(item.GetUID()),
		CreatedAt:       item.GetCreationTimestamp().Time.UTC(),
		Status:          resourceStatus(item),
		Ready:           resourceReady(item),
		Labels:          item.GetLabels(),
		ResourceVersion: item.GetResourceVersion(),
	}
}

// resourceStatus returns a compact status string for common resource shapes.
func resourceStatus(item *unstructured.Unstructured) string {
	if phase, found, _ := unstructured.NestedString(item.Object, "status", "phase"); found && phase != "" {
		return phase
	}
	if conditions, found, _ := unstructured.NestedSlice(item.Object, "status", "conditions"); found {
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]interface{})
			if !ok {
				continue
			}
			conditionType, _, _ := unstructured.NestedString(condition, "type")
			conditionStatus, _, _ := unstructured.NestedString(condition, "status")
			if conditionType == "Ready" || conditionType == "Available" || conditionType == "Progressing" {
				return conditionStatus
			}
		}
	}
	return "Active"
}

// resourceReady returns a ready/desired pair for common workload resources.
func resourceReady(item *unstructured.Unstructured) string {
	ready := nestedInt64(item.Object, "status", "readyReplicas")
	if ready == 0 {
		ready = nestedInt64(item.Object, "status", "availableReplicas")
	}
	desired := nestedInt64(item.Object, "spec", "replicas")
	if desired > 0 || ready > 0 {
		return fmt.Sprintf("%d/%d", ready, desired)
	}
	return ""
}

// nestedInt64 reads an integer field from an unstructured object.
func nestedInt64(object map[string]interface{}, fields ...string) int64 {
	value, found, _ := unstructured.NestedInt64(object, fields...)
	if !found {
		return 0
	}
	return value
}

// buildNodeOverview calculates node health counters.
func buildNodeOverview(items []unstructured.Unstructured) NodeOverview {
	overview := NodeOverview{Total: len(items)}
	for index := range items {
		item := &items[index]
		unschedulable, _, _ := unstructured.NestedBool(item.Object, "spec", "unschedulable")
		if unschedulable {
			overview.Cordoned++
		}
		ready := false
		pressure := false
		conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]interface{})
			if !ok {
				continue
			}
			conditionType, _, _ := unstructured.NestedString(condition, "type")
			conditionStatus, _, _ := unstructured.NestedString(condition, "status")
			if conditionType == "Ready" && conditionStatus == "True" {
				ready = true
			}
			if strings.HasSuffix(conditionType, "Pressure") && conditionStatus == "True" {
				pressure = true
			}
		}
		if ready {
			overview.Ready++
		} else {
			overview.NotReady++
		}
		if pressure {
			overview.Pressure++
		}
	}
	return overview
}

// buildWorkloadOverview calculates workload health counters.
func buildWorkloadOverview(deployments, statefulSets, daemonSets, jobs, cronJobs []unstructured.Unstructured) WorkloadOverview {
	overview := WorkloadOverview{
		Deployments:  len(deployments),
		StatefulSets: len(statefulSets),
		DaemonSets:   len(daemonSets),
		Jobs:         len(jobs),
		CronJobs:     len(cronJobs),
	}
	for index := range deployments {
		item := &deployments[index]
		if nestedInt64(item.Object, "status", "availableReplicas") >= nestedInt64(item.Object, "spec", "replicas") {
			overview.HealthyDeployments++
		}
	}
	for index := range statefulSets {
		item := &statefulSets[index]
		if nestedInt64(item.Object, "status", "readyReplicas") >= nestedInt64(item.Object, "spec", "replicas") {
			overview.HealthyStatefulSets++
		}
	}
	for index := range daemonSets {
		item := &daemonSets[index]
		if nestedInt64(item.Object, "status", "numberReady") >= nestedInt64(item.Object, "status", "desiredNumberScheduled") {
			overview.HealthyDaemonSets++
		}
	}
	for index := range jobs {
		item := &jobs[index]
		if nestedInt64(item.Object, "status", "failed") > 0 {
			overview.FailedJobs++
		}
	}
	return overview
}

// buildPodOverview calculates Pod phase and restart counters.
func buildPodOverview(items []unstructured.Unstructured) PodOverview {
	overview := PodOverview{Total: len(items)}
	for index := range items {
		item := &items[index]
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		switch phase {
		case "Running":
			overview.Running++
		case "Pending":
			overview.Pending++
		case "Failed":
			overview.Failed++
		}
		statuses, _, _ := unstructured.NestedSlice(item.Object, "status", "containerStatuses")
		restarts := int64(0)
		for _, rawStatus := range statuses {
			status, ok := rawStatus.(map[string]interface{})
			if !ok {
				continue
			}
			restarts += nestedInt64(status, "restartCount")
		}
		if restarts > 0 {
			overview.Restarting++
		}
	}
	return overview
}

// buildStorageOverview calculates PersistentVolumeClaim phase counters.
func buildStorageOverview(items []unstructured.Unstructured) StorageOverview {
	overview := StorageOverview{Total: len(items)}
	for index := range items {
		phase, _, _ := unstructured.NestedString(items[index].Object, "status", "phase")
		switch phase {
		case "Bound":
			overview.Bound++
		case "Pending":
			overview.Pending++
		case "Lost":
			overview.Lost++
		}
	}
	return overview
}

// buildWarningEvents selects and sorts recent warning Events.
func buildWarningEvents(items []unstructured.Unstructured) []EventSummary {
	warnings := make([]EventSummary, 0)
	for index := range items {
		item := &items[index]
		eventType, _, _ := unstructured.NestedString(item.Object, "type")
		if eventType != "Warning" {
			continue
		}
		reason, _, _ := unstructured.NestedString(item.Object, "reason")
		message, _, _ := unstructured.NestedString(item.Object, "message")
		namespace := item.GetNamespace()
		objectName := ""
		involvedObject, _, _ := unstructured.NestedMap(item.Object, "involvedObject")
		if involvedObject != nil {
			kind, _, _ := unstructured.NestedString(involvedObject, "kind")
			name, _, _ := unstructured.NestedString(involvedObject, "name")
			objectName = kind + "/" + name
		}
		lastSeen := eventTime(item)
		warnings = append(warnings, EventSummary{Type: eventType, Reason: reason, Message: message, Namespace: namespace, Object: objectName, LastSeen: lastSeen})
	}
	sort.Slice(warnings, func(i, j int) bool { return warnings[i].LastSeen.After(warnings[j].LastSeen) })
	if len(warnings) > 8 {
		warnings = warnings[:8]
	}
	return warnings
}

// eventTime extracts the best available timestamp from an Event.
func eventTime(item *unstructured.Unstructured) time.Time {
	for _, field := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		value, found, _ := unstructured.NestedString(item.Object, field)
		if !found || value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
	}
	return item.GetCreationTimestamp().Time.UTC()
}
