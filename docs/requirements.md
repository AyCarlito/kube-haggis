# KubeHaggis Requirements

## Product

KubeHaggis is a personal, internal-only web console for managing Kubernetes clusters through the native Kubernetes API. It runs as a local web server and reads the host kubeconfig. It does not run inside a cluster.

The application is desktop-first and responsive. The application name is `KubeHaggis`; `kube-haggis` is used where a filesystem, command, or package name needs a kebab-case form. The provided `haggis.svg` is the product mark and must remain part of the UI.

## Implementation Status

The repository contains the completed agreed first-release operation set. The backend and browser UI are real Kubernetes integrations, not mock data. Planned follow-on requirements are documented separately below and are not yet implemented.

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
- Resource list status filters, page-local sorting, configurable summary columns, bounded page sizes, and opaque continuation-token pagination.
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
- Ephemeral debug-container launch through `pods/ephemeralcontainers` and bounded native file download/upload through `pods/exec`.
- Native `metrics.k8s.io/v1beta1` collection with node and Pod CPU/memory usage summaries, freshness states, and responsive overview charts.
- Discovery-driven related-resource views for owner descendants, selectors, storage bindings, and safe native PodSpec references with explicit partial/RBAC states.
- Targeted workload operations for scale, restart, pause/resume, rollback, image changes, and Job/CronJob scheduling with dry-run review and confirmation.
- Native Node cordon, uncordon, drain, and taint workflows with eviction-only drain safety review and explicit confirmation.
- PVC resize and VolumeSnapshot workflows with StorageClass/API capability states, dry-run review, and explicit confirmation.
- High-friction single and bulk delete workflows with resource-version preconditions, server-side dry-run review, exact confirmations, per-item results, and redacted audit outcomes.
- Schema-generated forms for supported structural CRDs with bounded validation and YAML fallback for unsupported schemas.
- Kubeconfig reload controls and metadata-only file-change detection with last-good-state retention and safe context refresh.
- Read-only local mutation history UI with bounded filters, safe outcome summaries, refresh states, and disconnected-source handling.
- Deterministic fake-cluster and HTTP handler coverage for discovery, watches, mutations, permission failures, conflicts, dry-runs, and sensitive-error redaction.
- Dedicated resource-version conflict review with safe current-object refresh, bounded difference summaries, local-draft preservation, and explicit retry preparation.
- Local-only visual identity using the provided `haggis.svg` asset.
- Revision-scoped discovery caching, bounded related-resource query planning, deferred reverse-owner scans, safe relationship diagnostics, and large-discovery benchmark coverage.
- Complete discovery-backed GVR catalog with subresource capabilities, partial-discovery states, scope validation, and canonical resource routes.
- Version-aware CRD capabilities with served/preferred/storage metadata, bounded printer columns, subresource details, and schema-form YAML fallbacks.
- Reliable resource and Event watches with bounded relist recovery, freshness states, cancellation, and safe failure signals.

### Partially Implemented

- Resource detail pages have overview, YAML, live Events, and discovery-driven related-resource relationship views.
- Event detail pages omit the Events tab because Events are not expected to have child Events.
- CRDs support discovery, generic CRUD, schema-generated forms for supported structural schemas, and YAML editing as the complete fallback.
- Client-side apply currently uses full-object Kubernetes updates rather than a three-way `kubectl apply` merge. Resource-version conflicts use the dedicated review workflow and remain explicit rather than being force-resolved.

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
- Require write mode for ephemeral debug-container creation and file uploads; allow bounded file downloads in read-only mode under Kubernetes RBAC.
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

## Planned Follow-On Requirements

The following requirements extend the completed first-release scope. Each heading indicates whether the follow-on requirement is implemented or planned.

### F1. Related-resource query performance (Implemented)

Improve the performance and predictability of the related-resource view without removing relationship coverage.

- Cache API discovery per kubeconfig context and configuration revision, with single-flight loading and explicit invalidation after reload.
- Resolve direct owners, selectors, storage bindings, and explicit references before attempting broad reverse-owner discovery.
- Memoize target lookups within one relationship request and cap concurrent Kubernetes calls.
- Enforce a per-request deadline and Kubernetes-call budget; an initial target is a two-second full-request deadline and no more than 32 Kubernetes calls.
- Defer broad reverse-owner discovery to a secondary operation when it would delay the initial detail view.
- Return partial or truncated relationship results with an explicit reason instead of failing the complete panel.
- Include safe query-count and timing information for diagnostics.
- Add a benchmark fixture with at least 1,000 discovered resources and establish a p95 latency target for direct relationships.

### F2. Complete discovery-backed resource catalog (Implemented)

Expose every discoverable built-in and custom top-level resource through a complete, unambiguous resource catalog.

- Display resources grouped by API group, version, kind, scope, and resource name.
- Use complete group/version/resource identity in routes and API requests; preserve existing short routes only when they are unambiguous.
- Show discovered verbs, short names, categories, and supported subresource capabilities.
- Treat subresources such as `status`, `scale`, `exec`, and `log` as capabilities rather than ordinary listable resources.
- Derive namespace scope from discovery and reject caller-supplied scope values that contradict discovery.
- Surface partial discovery, unavailable APIs, and RBAC-denied resources explicitly with retry or refresh actions.
- Handle clusters with large numbers of CRDs without silently truncating the catalog.
- Keep generic CRUD and YAML fallback available without adding a typed backend implementation for every resource kind.

### F3. Version-aware CRD and custom-resource capabilities (Implemented)

Make CRD metadata useful for browsing, forms, and resource operations while keeping CRD installation and schema mutation out of scope.

- Show CRD group, kind, plural, scope, served versions, preferred version, and storage version.
- Detect and expose `status` and `scale` subresources and make status-only fields visibly read-only where applicable.
- Use safe CRD printer columns in custom-resource list views when available.
- Generate forms from supported structural schemas and explain field-level fallbacks for unsupported schema constructs.
- Preserve YAML as the complete fallback for recursive, union, preserve-unknown, or otherwise unsupported schemas.
- Handle custom resources across all served versions without confusing version identity or resource-version concurrency.
- Keep CRD installation, schema editing, and CRD deletion safeguards as separate future work.

### F4. Reliable watches and freshness states (Implemented)

Make live resource and Event data resilient to network failures, watch closure, and expired resource versions.

- Reconnect closed watches with bounded exponential backoff.
- Relist before restarting after resource-version expiration, HTTP 410 responses, or an unrecoverable watch error.
- Distinguish live, reconnecting, stale, refreshing, and unavailable states in the UI.
- Display the last successful refresh time for watched panels.
- Cancel watches deterministically on navigation, context changes, kubeconfig reloads, and component disposal.
- Cover watch closure, relist, HTTP 410, network recovery, and cancellation with deterministic tests.

### F5. Built-in operational read packs

Add structured, read-only views for high-value built-in resources while retaining generic YAML, Events, and related-resource fallback.

- Add operational views for Ingress and EndpointSlice backend visibility.
- Add NetworkPolicy summaries and affected Pod selector visibility.
- Add HorizontalPodAutoscaler target and status visibility.
- Add PodDisruptionBudget status and disruption-allowance visibility.
- Add ResourceQuota and LimitRange usage summaries.
- Add PersistentVolume and StorageClass relationship and lifecycle visibility.
- Add ServiceAccount and read-only RBAC relationship visibility.
- Add richer Pod scheduling, readiness, probes, termination reasons, volumes, and security-context summaries.
- Show missing APIs, unsupported versions, and RBAC denial per panel without hiding unrelated information.
- Use the generic dynamic resource layer and bounded extractors rather than a typed backend implementation for every kind.

### F6. Global search and saved views

Provide a bounded cross-resource search workflow for operators working across namespaces and resource kinds.

- Search discovered resources by API group, kind, namespace, name, and labels.
- Clearly identify partial results, loading states, and RBAC limitations.
- Preserve search, scope, and filter state in deep-linkable URLs.
- Allow locally persisted saved views for common resource, namespace, and label-selector combinations.
- Do not issue an unbounded request to every discovered resource on every keystroke; use debouncing, bounded fan-out, or a controlled cache.
- Keep cluster context and kubeconfig revision associated with saved views so stale views fail safely.

### F7. Server-bound mutation review

Ensure final mutations cannot bypass or reuse an unrelated client-side dry-run review.

- Return a short-lived opaque review handle or equivalent server-verifiable token from a successful review.
- Bind the review to context, group/version/resource, namespace/name, expected resource version, action parameters, and a sanitized body hash where applicable.
- Require the review handle for the final mutation and reject missing, expired, stale, or mismatched reviews.
- Invalidate review state after resource refresh, context switch, write-mode reset, or draft/action changes.
- Preserve existing Secret masking and redaction rules in reviews, diffs, conflicts, logs, and audit history.
- Keep force deletion and automatic conflict overwrites unavailable unless separately approved by the requirements.

### F8. Local diagnostics and API health visibility

Provide safe, local-only diagnostics for discovery, watches, API latency, stream sessions, and backend failures.

- Assign a correlation ID to every HTTP request and return it in `X-Request-ID`.
- Include safe request method, status, duration, context, resource identity, operation class, and error-category fields in backend logs.
- Never include request bodies, Secret values, credentials, authorization headers, Pod output, or raw sensitive Kubernetes status data in diagnostics.
- Add a diagnostics endpoint and UI panel showing kubeconfig revision, selected context, discovery-cache age, watch status, recent safe error categories, active native sessions, and audit-store availability.
- Show correlation IDs and safe retry classifications in UI failures.
- Distinguish liveness from dependency/readiness state for local health checks.
