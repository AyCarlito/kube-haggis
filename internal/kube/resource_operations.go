package kube

// MaxBulkResourceDeletes bounds one generic bulk-delete review and mutation.
const MaxBulkResourceDeletes = 100

// ResourceDeleteOptions describes the safe Kubernetes delete options exposed by the generic API.
type ResourceDeleteOptions struct {
	DryRun                  bool
	Force                   bool
	ExpectedResourceVersion string
}

// ResourceDeleteItem identifies one selected resource for a generic bulk delete.
type ResourceDeleteItem struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace,omitempty"`
	ResourceVersion string `json:"resourceVersion"`
}

// BulkDeleteRequest contains only bounded resource coordinates and confirmation state.
type BulkDeleteRequest struct {
	Items        []ResourceDeleteItem `json:"items"`
	DryRun       bool                 `json:"dryRun"`
	Confirmed    bool                 `json:"confirmed"`
	Confirmation string               `json:"confirmation,omitempty"`
	Force        bool                 `json:"force,omitempty"`
}

// BulkDeleteItemResult reports one redacted generic delete outcome.
type BulkDeleteItemResult struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Success   bool   `json:"success"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// BulkDeleteResponse reports the bounded summary and per-item outcomes of a bulk delete.
type BulkDeleteResponse struct {
	Resource  string                 `json:"resource"`
	Namespace string                 `json:"namespace,omitempty"`
	DryRun    bool                   `json:"dryRun"`
	Total     int                    `json:"total"`
	Succeeded int                    `json:"succeeded"`
	Failed    int                    `json:"failed"`
	Status    string                 `json:"status"`
	Items     []BulkDeleteItemResult `json:"items"`
}
