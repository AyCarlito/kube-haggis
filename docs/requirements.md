# KubeHaggis Requirements

## Product

KubeHaggis is a personal, internal-only web console for managing Kubernetes clusters through the native Kubernetes API. It runs as a local web server and reads the host kubeconfig. It does not run inside a cluster.

The application is desktop-first and responsive. The application name is `KubeHaggis`; `kube-haggis` is used where a filesystem, command, or package name needs a kebab-case form. The provided `haggis.svg` is the product mark and must remain part of the UI.

## Implementation Status

The repository currently contains the first usable vertical slice. The backend and browser UI are real Kubernetes integrations, not mock data, but the full agreed first-release operation set is not complete yet.

### Implemented

- Local Go web server with React and TypeScript frontend.
- Standard `KUBECONFIG` and `~/.kube/config` loading.
- Current-context startup and kubeconfig context switching.
- Deep-linkable cluster, namespace, resource list, and resource detail routes.
- Cluster overview landing page with node, Pod, workload, storage, namespace, and warning Event summaries.
- Per-context namespace persistence with an all-namespaces fallback.
- Discovery-driven built-in resource and CRD browsing through Kubernetes dynamic clients.
- Generic resource list, detail, create, update, delete, and watch API paths.
- Resource list search, namespace scope, label selector, and live-watch refresh behavior.
- Generic YAML editor and create workflow.
- Client-side full-object updates with resource-version handling and Kubernetes dry-run review.
- Refresh the complete resource from Kubernetes after a successful YAML apply before updating the editor state.
- Resource-scoped Events with live watches and navigation into generic Event details.
- Owner-reference relationship view with controller/deletion flags and discovery-aware navigation to owner resources.
- Per-context read-only/write-mode UI and backend write-mode guard.
- Secret value masking with explicit reveal.
- Removal of `metadata.managedFields` from the YAML editor and write payloads.
- Redacted SQLite mutation history for apply and delete outcomes.
- Native Pod log streaming with follow, previous-container, tail, and container controls.
- Native interactive Pod `exec` and `attach` sessions available in read-only mode with TTY input, resize handling, container selection, and Kubernetes RBAC authorization.
- Native Pod port-forward sessions with loopback binding, target/local port controls, session lifecycle, and Kubernetes RBAC authorization.
- Local-only visual identity using the provided `haggis.svg` asset.

### Partially Implemented

- Resource tables have search, namespace, label selector, and live updates, but status filters, sorting, configurable columns, and continuation-token pagination are still incomplete.
- Resource detail pages have overview, YAML, live Events, and owner-reference relationship views. Broader related-resource tabs are still explanatory placeholders.
- Event detail pages omit the Events tab because Events are not expected to have child Events.
- CRDs support discovery, generic CRUD, and YAML editing. Schema-generated CRD forms are not implemented yet.
- Client-side apply currently uses full-object Kubernetes updates rather than a three-way `kubectl apply` merge. Resource-version conflicts reach the UI as errors, but there is no dedicated conflict-resolution workflow yet.
- The overview detects whether `metrics.k8s.io` is available, but it does not yet fetch or chart live usage.
- Mutation history is persisted and exposed by the backend API, but it does not yet have a dedicated history view in the UI.

### Remaining First-Release Work

- Ephemeral debug containers and file copy operations.
- Metrics API collection and CPU/memory charts.
- Related-resource relationship views beyond standard `metadata.ownerReferences`.
- Targeted workload forms and actions for scale, restart, pause/resume, rollback, image changes, and job scheduling.
- Node cordon, uncordon, drain, and taint workflows.
- PVC resize and snapshot workflows.
- Delete confirmations, bulk operations, and stronger destructive-action review.
- Schema-generated forms for structural CRDs.
- Kubeconfig reload controls and file-change handling.
- UI for local mutation history.
- Fake-cluster and handler tests covering discovery, watches, mutations, and permission failures.

### Verification

- `go test ./...`
- `go vet ./...`
- `npm run build` from `web/`
- Local health, context, audit, frontend, and `haggis.svg` endpoints manually verified.

## Cluster And Navigation Behavior

- Use the standard `KUBECONFIG` loading rules, falling back to `~/.kube/config`.
- Start from kubeconfig `current-context` on every application start.
- List all kubeconfig contexts in the cluster switcher.
- Make contexts, namespaces, resource lists, and resource details deep-linkable.
- Use the cluster overview as the landing page.
- Persist namespace selection per cluster context across resource pages.
- Use `All Namespaces` when a cluster has no remembered namespace or the remembered namespace no longer exists.
- Keep cluster-scoped resources independent from the selected namespace.
- Use live Kubernetes watches with reconnect and refresh fallback.

## Resource Experience

- Discover built-in resources and CRDs from the Kubernetes API.
- Provide common list controls: search, namespace scope, label selectors, status filters, sorting, pagination, configurable columns, and live updates.
- Use tabbed detail pages with structured overview, YAML, Events, related resources, and resource-specific operational views.
- Make YAML editing available for every resource.
- Add targeted forms for common workload operations.
- Generate CRD forms from structural schemas when possible, with YAML as the complete fallback.
- Use the Kubernetes metrics API when available; do not add Prometheus or other external integrations in the native-only scope.

## Safety And Access

- Bind to `127.0.0.1` by default.
- Do not expose kubeconfig contents or credentials to the browser.
- Do not add application-level login in the first release.
- Use the selected kubeconfig identity and Kubernetes RBAC as the authorization authority.
- Start in read-only mode.
- Maintain write mode independently per cluster context and reset it on context switch unless explicitly enabled again.
- Allow interactive Pod `exec` and `attach` sessions in read-only mode; Kubernetes RBAC remains the authorization authority for those sessions.
- Allow Pod port-forward sessions in read-only mode; bind local endpoints to loopback and rely on Kubernetes RBAC for authorization.
- Require a diff and server-side dry-run before applying edits where supported.
- Use client-side apply/full-object updates for writes, with Kubernetes resource-version conflicts surfaced to the user.
- Require stronger confirmation for deletes, evictions, drains, namespace changes, and other destructive operations.
- Redact Secret values by default and require explicit reveal for sensitive data.
- Persist a redacted local mutation history in SQLite without Secret values.

## First Release Native Operations

The initial implementation target is the following operation set:

```text
G1-G23
P1-P7
D1-D6
SS1-SS4
DS1-DS3
J1-J4
CJ1-CJ3
N1-N5
NS1-NS4
C1-C4
NW1-NW10
ST1-ST6
OP1-OP14
```

The generic API layer must work with built-in resources and CRDs without requiring resource-specific backend code.

## Explicitly Out Of Scope

- Helm
- Argo CD
- Flux
- Cloud-provider operations
- Cluster creation, upgrade, and deletion
- Other external control-plane integrations

## Future Native Mutation Backlog

- Generic resource cloning and copying between namespaces
- YAML export workflows
- Automatic resource-version conflict resolution
- Bulk label and annotation workflows
- Advanced Deployment field-specific actions for images, environment variables, resources, probes, volumes, scheduling, and service accounts
- StatefulSet update partitions, advanced update strategies, and Pod management policy controls
- DaemonSet scheduling and Pod template convenience editors
- ReplicaSet and ReplicationController scaling and ownership operations
- Job retry/completion configuration and advanced rerun workflows
- CronJob schedule, concurrency, history, deadline, and Job-template editors
- Manual Pod binding to a Node
- Advanced Pod resize workflows
- Node label and annotation editing
- Node object deletion
- Advanced drain planning for affected or pending Pods
- Force-finalizing stuck Namespaces
- Creating ConfigMaps and Secrets from files or directories
- Secret rotation and generated-value workflows
- Previewing workloads affected by ConfigMap or Secret changes
- Gateway API resource management
- Direct Endpoint and EndpointSlice management
- StorageClass management
- PersistentVolume attachment management
- Advanced volume expansion and binding controls
- Specialized RBAC editors for roles, rules, bindings, and subjects
- ServiceAccount token creation
- CertificateSigningRequest approval and denial
- Admission policy and webhook management
- CRD installation, schema editing, and deletion safeguards
- Custom-resource-specific actions exposed through configured Kubernetes subresources
