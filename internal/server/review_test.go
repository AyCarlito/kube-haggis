package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AyCarlito/kube-haggis/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// reviewTestRequest creates one write-enabled mutation request for handler tests.
func reviewTestRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("X-KubeHaggis-Write", "enabled")
	return request
}

// reviewTestToken returns the opaque handle emitted by a successful dry-run response.
func reviewTestToken(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	token := response.Header().Get(mutationReviewHeader)
	if token == "" {
		t.Fatalf("successful dry-run did not return a review handle: status=%d body=%s", response.Code, response.Body.String())
	}
	return token
}

// TestMutationReviewStoreExpiresAndConsumesOnce verifies deterministic expiry, mismatch retention, and one-use behavior.
func TestMutationReviewStoreExpiresAndConsumesOnce(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := newMutationReviewStore()
	store.now = func() time.Time { return now }
	binding := reviewBinding{Context: "field", Action: "apply", ParametersHash: hashReviewParameters("safe-parameters")}
	token, err := store.issue(binding)
	if err != nil || token == "" || strings.Contains(token, "safe-parameters") {
		t.Fatalf("issue returned an unsafe or missing token: token=%q err=%v", token, err)
	}
	mismatch := binding
	mismatch.Action = "delete"
	if err := store.consume(token, mismatch); err == nil {
		t.Fatal("mismatched review was accepted")
	}
	if err := store.consume(token, binding); err != nil {
		t.Fatalf("matching review was not accepted after a mismatch: %v", err)
	}
	if err := store.consume(token, binding); err == nil {
		t.Fatal("review handle was reusable after consumption")
	}

	expiringToken, err := store.issue(binding)
	if err != nil {
		t.Fatalf("issue expiring review: %v", err)
	}
	now = now.Add(mutationReviewTTL)
	if err := store.consume(expiringToken, binding); err == nil {
		t.Fatal("expired review was accepted")
	}
}

// TestMutationReviewBindingRejectsEveryBoundField verifies every exact binding component is enforced.
func TestMutationReviewBindingRejectsEveryBoundField(t *testing.T) {
	base := reviewBinding{
		Context:                 "field",
		Group:                   "apps",
		Version:                 "v1",
		Resource:                "deployments",
		Namespace:               "default",
		Name:                    "api",
		Namespaced:              true,
		ExpectedResourceVersion: "7",
		Action:                  "workload-action",
		ParametersHash:          hashReviewParameters("action-parameters"),
		BodyHash:                hashReviewParameters("sanitized-body"),
		HasBodyHash:             true,
		ItemsHash:               hashReviewParameters("ordered-items"),
		HasItemsHash:            true,
		ConfirmationScope:       "DELETE 2",
	}
	tests := []struct {
		name   string
		mutate func(*reviewBinding)
	}{
		{name: "context", mutate: func(binding *reviewBinding) { binding.Context = "other" }},
		{name: "group", mutate: func(binding *reviewBinding) { binding.Group = "batch" }},
		{name: "version", mutate: func(binding *reviewBinding) { binding.Version = "v2" }},
		{name: "resource", mutate: func(binding *reviewBinding) { binding.Resource = "statefulsets" }},
		{name: "namespace", mutate: func(binding *reviewBinding) { binding.Namespace = "other" }},
		{name: "name", mutate: func(binding *reviewBinding) { binding.Name = "other" }},
		{name: "scope", mutate: func(binding *reviewBinding) { binding.Namespaced = false }},
		{name: "resource version", mutate: func(binding *reviewBinding) { binding.ExpectedResourceVersion = "8" }},
		{name: "action", mutate: func(binding *reviewBinding) { binding.Action = "delete" }},
		{name: "parameters", mutate: func(binding *reviewBinding) { binding.ParametersHash = hashReviewParameters("changed-parameters") }},
		{name: "body hash", mutate: func(binding *reviewBinding) { binding.BodyHash = hashReviewParameters("changed-body") }},
		{name: "item hash", mutate: func(binding *reviewBinding) { binding.ItemsHash = hashReviewParameters("changed-items") }},
		{name: "confirmation scope", mutate: func(binding *reviewBinding) { binding.ConfirmationScope = "DELETE 3" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMutationReviewStore()
			token, err := store.issue(base)
			if err != nil {
				t.Fatalf("issue review: %v", err)
			}
			candidate := base
			test.mutate(&candidate)
			if err := store.consume(token, candidate); err == nil {
				t.Fatal("binding mismatch was accepted")
			}
			if err := store.consume(token, base); err != nil {
				t.Fatalf("matching binding was consumed by a mismatch: %v", err)
			}
		})
	}
}

// TestGenericApplyReviewBindsBodyAndConsumesBeforeMutation verifies generic apply body binding and bypass rejection.
func TestGenericApplyReviewBindsBodyAndConsumesBeforeMutation(t *testing.T) {
	calls := 0
	server := &Server{
		resourceApply: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ bool, dryRun bool, _ []byte) (*unstructured.Unstructured, error) {
			calls++
			return &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]interface{}{"name": "credentials", "namespace": "default", "resourceVersion": "8"},
				"dryRun":     dryRun,
			}}, nil
		},
	}
	body := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: credentials\n  namespace: default\n  resourceVersion: \"7\"\n  managedFields:\n    - manager: server\ndata:\n  password: private-value\n"
	reviewResponse := httptest.NewRecorder()
	server.handleResource(reviewResponse, reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&dryRun=true", body))
	token := reviewTestToken(t, reviewResponse)
	if strings.Contains(token, "private-value") || strings.Contains(reviewResponse.Body.String(), "private-value") {
		t.Fatalf("review response exposed request material: token=%q body=%s", token, reviewResponse.Body.String())
	}

	changedBody := strings.Replace(body, "private-value", "changed-value", 1)
	changedResponse := httptest.NewRecorder()
	changedRequest := reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&dryRun=false", changedBody)
	changedRequest.Header.Set(mutationReviewHeader, token)
	server.handleResource(changedResponse, changedRequest)
	if changedResponse.Code != http.StatusBadRequest || calls != 1 || strings.Contains(changedResponse.Body.String(), "changed-value") {
		t.Fatalf("changed generic body bypassed review: status=%d calls=%d body=%s", changedResponse.Code, calls, changedResponse.Body.String())
	}

	finalResponse := httptest.NewRecorder()
	finalRequest := reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&dryRun=false", body)
	finalRequest.Header.Set(mutationReviewHeader, token)
	server.handleResource(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusOK || calls != 2 {
		t.Fatalf("matching generic apply was not dispatched: status=%d calls=%d body=%s", finalResponse.Code, calls, finalResponse.Body.String())
	}
	retryResponse := httptest.NewRecorder()
	retryRequest := reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=&version=v1&resource=secrets&namespace=default&name=credentials&namespaced=true&dryRun=false", body)
	retryRequest.Header.Set(mutationReviewHeader, token)
	server.handleResource(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusBadRequest || calls != 2 {
		t.Fatalf("generic apply review was reusable: status=%d calls=%d body=%s", retryResponse.Code, calls, retryResponse.Body.String())
	}
}

// TestNamedGenericApplyRequiresResourceVersionBeforeBackend verifies missing versions are rejected for review and final requests.
func TestNamedGenericApplyRequiresResourceVersionBeforeBackend(t *testing.T) {
	calls := 0
	server := &Server{
		resourceApply: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ bool, _ bool, _ []byte) (*unstructured.Unstructured, error) {
			calls++
			return &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "api"}}}, nil
		},
	}
	body := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  replicas: 1\n"
	reviewResponse := httptest.NewRecorder()
	server.handleResource(reviewResponse, reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&dryRun=true", body))
	if reviewResponse.Code != http.StatusBadRequest || calls != 0 || !strings.Contains(reviewResponse.Body.String(), "resourceVersion") {
		t.Fatalf("missing-version named review reached backend or returned unsafe validation: status=%d calls=%d body=%s", reviewResponse.Code, calls, reviewResponse.Body.String())
	}
	finalRequest := reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&dryRun=false", body)
	finalRequest.Header.Set(mutationReviewHeader, "invalid-review-token")
	finalResponse := httptest.NewRecorder()
	server.handleResource(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusBadRequest || calls != 0 || !strings.Contains(finalResponse.Body.String(), "resourceVersion") {
		t.Fatalf("missing-version named final reached backend or returned unsafe validation: status=%d calls=%d body=%s", finalResponse.Code, calls, finalResponse.Body.String())
	}
}

// TestOmittedQueryNameNamedBodyRequiresResourceVersion verifies document metadata supplies the effective update name.
func TestOmittedQueryNameNamedBodyRequiresResourceVersion(t *testing.T) {
	calls := 0
	server := &Server{
		resourceApply: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ bool, _ bool, _ []byte) (*unstructured.Unstructured, error) {
			calls++
			return &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"name": "api"}}}, nil
		},
	}
	body := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  replicas: 1\n"
	reviewResponse := httptest.NewRecorder()
	server.handleResource(reviewResponse, reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&namespaced=true&dryRun=true", body))
	if reviewResponse.Code != http.StatusBadRequest || calls != 0 || !strings.Contains(reviewResponse.Body.String(), "resourceVersion") {
		t.Fatalf("named document without query name reached backend or returned unsafe validation: status=%d calls=%d body=%s", reviewResponse.Code, calls, reviewResponse.Body.String())
	}
	finalRequest := reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&namespaced=true&dryRun=false", body)
	finalRequest.Header.Set(mutationReviewHeader, "invalid-review-token")
	finalResponse := httptest.NewRecorder()
	server.handleResource(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusBadRequest || calls != 0 || !strings.Contains(finalResponse.Body.String(), "resourceVersion") {
		t.Fatalf("named document final reached backend or returned unsafe validation: status=%d calls=%d body=%s", finalResponse.Code, calls, finalResponse.Body.String())
	}
}

// TestGenerateNameApplyWithoutResourceVersionRemainsReviewable verifies creation keeps its existing no-version path.
func TestGenerateNameApplyWithoutResourceVersionRemainsReviewable(t *testing.T) {
	calls := 0
	server := &Server{
		resourceApply: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ bool, dryRun bool, _ []byte) (*unstructured.Unstructured, error) {
			calls++
			return &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]interface{}{"name": "settings-abc"},
				"dryRun":     dryRun,
			}}, nil
		},
	}
	body := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  generateName: settings-\n"
	response := httptest.NewRecorder()
	server.handleResource(response, reviewTestRequest(http.MethodPatch, "/api/resource?context=field&group=&version=v1&resource=configmaps&namespace=default&namespaced=true&dryRun=true", body))
	if response.Code != http.StatusOK || calls != 1 || response.Header().Get(mutationReviewHeader) == "" {
		t.Fatalf("generateName create was incorrectly blocked: status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
	}
}

// TestGenericDeleteReviewRejectsBypassAndBindsResourceVersion verifies generic delete handle enforcement.
func TestGenericDeleteReviewRejectsBypassAndBindsResourceVersion(t *testing.T) {
	calls := 0
	server := &Server{resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
		calls++
		return nil
	}}
	withoutReview := reviewTestRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&confirmed=true&confirmName=api", "")
	withoutReviewResponse := httptest.NewRecorder()
	server.handleResource(withoutReviewResponse, withoutReview)
	if withoutReviewResponse.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("generic delete bypass reached Kubernetes: status=%d calls=%d", withoutReviewResponse.Code, calls)
	}
	reviewResponse := httptest.NewRecorder()
	server.handleResource(reviewResponse, reviewTestRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&dryRun=true", ""))
	token := reviewTestToken(t, reviewResponse)
	changedResponse := httptest.NewRecorder()
	changedRequest := reviewTestRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=8&confirmed=true&confirmName=api", "")
	changedRequest.Header.Set(mutationReviewHeader, token)
	server.handleResource(changedResponse, changedRequest)
	if changedResponse.Code != http.StatusBadRequest || calls != 1 {
		t.Fatalf("changed delete resource version bypassed review: status=%d calls=%d", changedResponse.Code, calls)
	}
	finalRequest := reviewTestRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&confirmed=true&confirmName=api", "")
	finalRequest.Header.Set(mutationReviewHeader, token)
	finalResponse := httptest.NewRecorder()
	server.handleResource(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusOK || calls != 2 {
		t.Fatalf("matching delete review was not dispatched: status=%d calls=%d", finalResponse.Code, calls)
	}
}

// TestDryRunDoesNotConsumeEarlierReview verifies a repeated dry-run leaves an earlier matching handle usable.
func TestDryRunDoesNotConsumeEarlierReview(t *testing.T) {
	calls := 0
	server := &Server{resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
		calls++
		return nil
	}}
	target := "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7"
	firstResponse := httptest.NewRecorder()
	server.handleResource(firstResponse, reviewTestRequest(http.MethodDelete, target+"&dryRun=true", ""))
	firstToken := reviewTestToken(t, firstResponse)
	secondRequest := reviewTestRequest(http.MethodDelete, target+"&dryRun=true", "")
	secondRequest.Header.Set(mutationReviewHeader, firstToken)
	secondResponse := httptest.NewRecorder()
	server.handleResource(secondResponse, secondRequest)
	secondToken := reviewTestToken(t, secondResponse)
	if firstToken == secondToken {
		t.Fatal("repeated dry-run reused the same opaque review handle")
	}
	finalRequest := reviewTestRequest(http.MethodDelete, target+"&confirmed=true&confirmName=api", "")
	finalRequest.Header.Set(mutationReviewHeader, firstToken)
	finalResponse := httptest.NewRecorder()
	server.handleResource(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusOK || calls != 3 {
		t.Fatalf("earlier review was consumed by dry-run: status=%d calls=%d body=%s", finalResponse.Code, calls, finalResponse.Body.String())
	}
}

// TestBulkDeleteReviewBindsOrderedItems verifies bulk item order and resource versions cannot be changed after review.
func TestBulkDeleteReviewBindsOrderedItems(t *testing.T) {
	calls := 0
	server := &Server{resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
		calls++
		return nil
	}}
	items := []kube.ResourceDeleteItem{{Name: "one", Namespace: "default", ResourceVersion: "1"}, {Name: "two", Namespace: "default", ResourceVersion: "2"}}
	reviewBody, err := json.Marshal(kube.BulkDeleteRequest{Items: items, DryRun: true})
	if err != nil {
		t.Fatalf("marshal bulk review: %v", err)
	}
	reviewResponse := httptest.NewRecorder()
	server.handleBulkResourceDelete(reviewResponse, reviewTestRequest(http.MethodPost, "/api/resources/delete?context=field&group=&version=v1&resource=secrets&namespace=default&namespaced=true", string(reviewBody)))
	token := reviewTestToken(t, reviewResponse)
	reversed := []kube.ResourceDeleteItem{items[1], items[0]}
	changedBody, _ := json.Marshal(kube.BulkDeleteRequest{Items: reversed, DryRun: false, Confirmed: true, Confirmation: "DELETE 2"})
	changedRequest := reviewTestRequest(http.MethodPost, "/api/resources/delete?context=field&group=&version=v1&resource=secrets&namespace=default&namespaced=true", string(changedBody))
	changedRequest.Header.Set(mutationReviewHeader, token)
	changedResponse := httptest.NewRecorder()
	server.handleBulkResourceDelete(changedResponse, changedRequest)
	if changedResponse.Code != http.StatusBadRequest || calls != 2 {
		t.Fatalf("reordered bulk delete bypassed review: status=%d calls=%d", changedResponse.Code, calls)
	}
	finalBody, _ := json.Marshal(kube.BulkDeleteRequest{Items: items, DryRun: false, Confirmed: true, Confirmation: "DELETE 2"})
	finalRequest := reviewTestRequest(http.MethodPost, "/api/resources/delete?context=field&group=&version=v1&resource=secrets&namespace=default&namespaced=true", string(finalBody))
	finalRequest.Header.Set(mutationReviewHeader, token)
	finalResponse := httptest.NewRecorder()
	server.handleBulkResourceDelete(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusOK || calls != 4 {
		t.Fatalf("matching bulk delete review was not dispatched: status=%d calls=%d", finalResponse.Code, calls)
	}
}

// TestTargetedMutationReviewsBindWorkloadNodeAndPVC verifies the three native action families require their own exact handles.
func TestTargetedMutationReviewsBindWorkloadNodeAndPVC(t *testing.T) {
	t.Run("workload", func(t *testing.T) {
		calls := 0
		server := &Server{workloadAction: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ string, request kube.WorkloadActionRequest) (kube.WorkloadActionResult, error) {
			calls++
			return kube.WorkloadActionResult{Action: request.Action, Resource: "deployments", Namespace: "default", Name: "api", DryRun: request.DryRun, Status: map[bool]string{true: "dry-run passed", false: "applied"}[request.DryRun]}, nil
		}}
		reviewResponse := httptest.NewRecorder()
		server.handleWorkloadAction(reviewResponse, reviewTestRequest(http.MethodPost, "/api/workload/action?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api", `{"action":"image","dryRun":true,"expectedResourceVersion":"7","container":"app","image":"example:v1"}`))
		token := reviewTestToken(t, reviewResponse)
		changedRequest := reviewTestRequest(http.MethodPost, "/api/workload/action?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api", `{"action":"image","expectedResourceVersion":"7","container":"app","image":"example:v2"}`)
		changedRequest.Header.Set(mutationReviewHeader, token)
		changedResponse := httptest.NewRecorder()
		server.handleWorkloadAction(changedResponse, changedRequest)
		if changedResponse.Code != http.StatusBadRequest || calls != 1 {
			t.Fatalf("changed workload action bypassed review: status=%d calls=%d", changedResponse.Code, calls)
		}
		finalRequest := reviewTestRequest(http.MethodPost, "/api/workload/action?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api", `{"action":"image","expectedResourceVersion":"7","container":"app","image":"example:v1"}`)
		finalRequest.Header.Set(mutationReviewHeader, token)
		finalResponse := httptest.NewRecorder()
		server.handleWorkloadAction(finalResponse, finalRequest)
		if finalResponse.Code != http.StatusOK || calls != 2 {
			t.Fatalf("matching workload action was not dispatched: status=%d calls=%d", finalResponse.Code, calls)
		}
	})

	t.Run("node", func(t *testing.T) {
		calls := 0
		server := &Server{nodeAction: func(_ context.Context, _ string, _ string, request kube.NodeActionRequest) (kube.NodeActionResult, error) {
			calls++
			return kube.NodeActionResult{Action: request.Action, Resource: "nodes", Name: "worker-a", DryRun: request.DryRun, Ready: true, Status: map[bool]string{true: "dry-run passed", false: "applied"}[request.DryRun]}, nil
		}}
		reviewResponse := httptest.NewRecorder()
		server.handleNodeAction(reviewResponse, reviewTestRequest(http.MethodPost, "/api/node/action?context=field&name=worker-a", `{"action":"cordon","dryRun":true,"expectedResourceVersion":"7"}`))
		token := reviewTestToken(t, reviewResponse)
		changedRequest := reviewTestRequest(http.MethodPost, "/api/node/action?context=field&name=worker-a", `{"action":"uncordon","expectedResourceVersion":"7"}`)
		changedRequest.Header.Set(mutationReviewHeader, token)
		changedResponse := httptest.NewRecorder()
		server.handleNodeAction(changedResponse, changedRequest)
		if changedResponse.Code != http.StatusBadRequest || calls != 1 {
			t.Fatalf("changed Node action bypassed review: status=%d calls=%d", changedResponse.Code, calls)
		}
		finalRequest := reviewTestRequest(http.MethodPost, "/api/node/action?context=field&name=worker-a", `{"action":"cordon","expectedResourceVersion":"7"}`)
		finalRequest.Header.Set(mutationReviewHeader, token)
		finalResponse := httptest.NewRecorder()
		server.handleNodeAction(finalResponse, finalRequest)
		if finalResponse.Code != http.StatusOK || calls != 2 {
			t.Fatalf("matching Node action was not dispatched: status=%d calls=%d", finalResponse.Code, calls)
		}
	})

	t.Run("PVC", func(t *testing.T) {
		calls := 0
		server := &Server{pvcAction: func(_ context.Context, _ string, _ string, _ string, request kube.PVCActionRequest) (kube.PVCActionResult, error) {
			calls++
			return kube.PVCActionResult{Action: request.Action, Resource: "persistentvolumeclaims", Namespace: "default", Name: "data", DryRun: request.DryRun, Status: map[bool]string{true: "dry-run passed", false: "applied"}[request.DryRun]}, nil
		}}
		reviewResponse := httptest.NewRecorder()
		server.handlePVCAction(reviewResponse, reviewTestRequest(http.MethodPost, "/api/pvc/action?context=field&namespace=default&name=data", `{"action":"resize","dryRun":true,"expectedResourceVersion":"7","storage":"20Gi"}`))
		token := reviewTestToken(t, reviewResponse)
		changedRequest := reviewTestRequest(http.MethodPost, "/api/pvc/action?context=field&namespace=default&name=data", `{"action":"resize","confirmed":true,"expectedResourceVersion":"7","storage":"30Gi"}`)
		changedRequest.Header.Set(mutationReviewHeader, token)
		changedResponse := httptest.NewRecorder()
		server.handlePVCAction(changedResponse, changedRequest)
		if changedResponse.Code != http.StatusBadRequest || calls != 1 {
			t.Fatalf("changed PVC action bypassed review: status=%d calls=%d", changedResponse.Code, calls)
		}
		finalRequest := reviewTestRequest(http.MethodPost, "/api/pvc/action?context=field&namespace=default&name=data", `{"action":"resize","confirmed":true,"expectedResourceVersion":"7","storage":"20Gi"}`)
		finalRequest.Header.Set(mutationReviewHeader, token)
		finalResponse := httptest.NewRecorder()
		server.handlePVCAction(finalResponse, finalRequest)
		if finalResponse.Code != http.StatusOK || calls != 2 {
			t.Fatalf("matching PVC action was not dispatched: status=%d calls=%d", finalResponse.Code, calls)
		}
	})
}

// TestExpiredReviewIsRejectedBeforeGenericDeletion verifies expiry prevents a backend mutation call.
func TestExpiredReviewIsRejectedBeforeGenericDeletion(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	calls := 0
	server := &Server{
		reviewStore: func() *mutationReviewStore {
			store := newMutationReviewStore()
			store.now = func() time.Time { return now }
			return store
		}(),
		resourceDelete: func(context.Context, string, string, string, string, string, string, bool, kube.ResourceDeleteOptions) error {
			calls++
			return nil
		},
	}
	reviewResponse := httptest.NewRecorder()
	server.handleResource(reviewResponse, reviewTestRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&dryRun=true", ""))
	token := reviewTestToken(t, reviewResponse)
	now = now.Add(mutationReviewTTL)
	finalRequest := reviewTestRequest(http.MethodDelete, "/api/resource?context=field&group=apps&version=v1&resource=deployments&namespace=default&name=api&namespaced=true&expectedResourceVersion=7&confirmed=true&confirmName=api", "")
	finalRequest.Header.Set(mutationReviewHeader, token)
	finalResponse := httptest.NewRecorder()
	server.handleResource(finalResponse, finalRequest)
	if finalResponse.Code != http.StatusBadRequest || calls != 1 {
		t.Fatalf("expired review reached generic deletion: status=%d calls=%d body=%s", finalResponse.Code, calls, finalResponse.Body.String())
	}
}
