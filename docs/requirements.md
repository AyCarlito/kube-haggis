# KubeHaggis Requirements

## Product

KubeHaggis is a personal, internal-only web console for managing Kubernetes clusters through the native Kubernetes API. It runs as a local web server and reads the host kubeconfig. It does not run inside a cluster.

The application is desktop-first and responsive. The application name is `KubeHaggis`; `kube-haggis` is used where a filesystem, command, or package name needs a kebab-case form. The provided `haggis.svg` is the product mark and must remain part of the UI.

## Implementation Status

The repository contains the completed agreed first-release operation set. The backend and browser UI are real Kubernetes integrations, not mock data. Implemented follow-on requirements are documented below, and implemented UX improvements are documented separately at the end of this file.

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
- Bounded read-only operational packs for built-in networking, policy, autoscaling, quota, storage, ServiceAccount/RBAC, and Pod posture views.
- Bounded global search with canonical GVR result links, partial-source states, explicit submit behavior, and revision-bound local saved views.
- Short-lived server-bound mutation review handles with exact request bindings, one-use consumption, expiry, and frontend token clearing.
- Local diagnostics with correlation IDs, safe request telemetry, readiness/liveness separation, discovery/session health, and bounded error history.

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

### F5. Built-in operational read packs (Implemented)

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

### F6. Global search and saved views (Implemented)

Provide a bounded cross-resource search workflow for operators working across namespaces and resource kinds.

- Search discovered resources by API group, kind, namespace, name, and labels.
- Clearly identify partial results, loading states, and RBAC limitations.
- Expose bounded continuation pages across selected sources so free-text name matches are not limited to the first list page.
- Bind opaque search cursors to the active context, discovery revision, and normalized filters; expire them safely.
- Preserve search, scope, and filter state in deep-linkable URLs.
- Allow locally persisted saved views for common resource, namespace, and label-selector combinations.
- Do not issue an unbounded request to every discovered resource on every keystroke; use debouncing, bounded fan-out, or a controlled cache.
- Keep cluster context and kubeconfig revision associated with saved views so stale views fail safely.

### F7. Server-bound mutation review (Implemented)

Ensure final mutations cannot bypass or reuse an unrelated client-side dry-run review.

- Return a short-lived opaque review handle or equivalent server-verifiable token from a successful review.
- Bind the review to context, group/version/resource, namespace/name, expected resource version, action parameters, and a sanitized body hash where applicable.
- Require the review handle for the final mutation and reject missing, expired, stale, or mismatched reviews.
- Invalidate review state after resource refresh, context switch, write-mode reset, or draft/action changes.
- Preserve existing Secret masking and redaction rules in reviews, diffs, conflicts, logs, and audit history.
- Keep force deletion and automatic conflict overwrites unavailable unless separately approved by the requirements.

### F8. Local diagnostics and API health visibility (Implemented)

Provide safe, local-only diagnostics for discovery, watches, API latency, stream sessions, and backend failures.

- Assign a correlation ID to every HTTP request and return it in `X-Request-ID`.
- Include safe request method, status, duration, context, resource identity, operation class, and error-category fields in backend logs.
- Never include request bodies, Secret values, credentials, authorization headers, Pod output, or raw sensitive Kubernetes status data in diagnostics.
- Add a diagnostics endpoint and UI panel showing kubeconfig revision, selected context, discovery-cache age, watch status, recent safe error categories, active native sessions, and audit-store availability.
- Show correlation IDs and safe retry classifications in UI failures.
- Distinguish liveness from dependency/readiness state for local health checks.

## Implemented UX Requirements

The following requirements improve navigation, discoverability, readability, form coverage, and input behavior without changing the native Kubernetes safety model. They are completed and implemented.

### U1. Actionable overview navigation (Implemented)

Make overview summaries useful starting points rather than static status readouts.

- Make each overview metric card keyboard-accessible and clickable when it has a stable resource destination.
- Navigate `Nodes ready` to the canonical Nodes resource list, `Pods running` to the canonical Pods resource list, and `Storage claims` to the canonical PersistentVolumeClaim resource list.
- Do not route the aggregate `Workload health` card to one misleading kind. Open a workload-focused view that covers the represented Deployments, StatefulSets, and DaemonSets, with the existing jobs and schedules context available from the same view or an explicit adjacent destination.
- Preserve the selected kubeconfig context and namespace scope when navigating from an overview card. Use complete group/version/resource routes rather than guessed short routes.
- Show an obvious hover and focus treatment, an action affordance, and an accessible label that describes the destination. Do not make a card appear interactive when its target is unavailable.
- Apply the same navigation rule to overview posture rows and other summary values when an exact target exists, including Namespaces, bound claims, and Pod restart views. Keep aggregate or unresolved values static or provide an explicit aggregate destination rather than guessing.
- Keep loading, permission-denied, unavailable, and empty states visible; a missing destination must not become a dead click or a guessed resource route.

### U2. Signal check and Event navigation (Implemented)

Turn the landing-page warning panel into a reliable path to Event investigation.

- Make the Signal check panel action open the Events resource list in the active context and namespace scope.
- Make each warning Event row keyboard-accessible and open the exact Event detail route when its name and namespace are available.
- Preserve the Event identity as core `v1/events` coordinates, including namespace and name, so a warning cannot open a same-named object from the wrong namespace or API version.
- If an overview summary lacks enough identity for an individual route, open the bounded Events list with the available context rather than navigating to an incorrect detail page.
- Retain the existing Event watch, stale/reconnecting states, and RBAC/unavailable messaging after navigation.
- Use consistent link/button semantics, focus states, and labels for the panel action and individual Event rows.

### U3. Searchable and collapsible resource catalog (Implemented)

Make large discovery catalogs navigable without losing complete discovery coverage.

- Add a client-side catalog filter that searches API group, version, kind, resource name, short names, and categories; API group and kind are required search dimensions.
- Filter without making additional Kubernetes requests. Preserve partial discovery notices and do not hide an unavailable or RBAC-denied source without explaining why.
- Display the number of matching resources and an explicit no-match state. Provide a clear action that returns to the complete catalog.
- Allow each API group section to be collapsed or expanded independently with an accessible control and `aria-expanded` state.
- When a filter is active, automatically reveal groups containing matches so collapsing a section cannot conceal a search result. A cleared filter may restore the previous local expansion state.
- Keep catalog row navigation unchanged: selecting a row opens its exact group/version/resource route.
- Key any remembered expansion state by active context and catalog revision if it is persisted; stale expansion state must not affect catalog contents.

### U4. Contextual autocomplete for operator inputs (Implemented)

Use the already loaded discovery catalog to make structured inputs easier to complete without turning typing into unbounded Kubernetes activity.

- Add accessible combobox or datalist-style suggestions to Global Search for API group, Kind, and Resource.
- Populate API group suggestions from the discovered catalog and display the core API using a clear `Core API` label while retaining its empty group value internally.
- Constrain Kind suggestions by the selected API group. When no group is selected, show the group and version context alongside each suggestion so duplicate kinds are not ambiguous.
- Constrain Resource suggestions by the selected API group and Kind. Show enough group/version/kind context to distinguish duplicate resource names across served APIs.
- Refresh downstream suggestions when a parent filter changes. Do not silently substitute a different resource identity; preserve free-form text with a clear incompatible or no-match state, or provide an explicit clear action before submission.
- Offer bounded namespace suggestions from the selected context when that list is already available or can be loaded once and cached. Keep `All namespaces` and arbitrary namespace entry available for namespaces the current identity cannot enumerate.
- Keep the primary search text, object name, and label selector free-form. Do not query Kubernetes on every keystroke or build an unbounded object-name index.
- For label selectors, prefer syntax guidance and bounded key/value suggestions from currently loaded result data over cluster-wide live suggestions. Never rewrite an operator's selector without an explicit selection.
- Support keyboard navigation, selection, dismissal, screen-reader labels, and mobile touch targets. Suggestions assist entry but do not prevent valid custom-resource or namespace values.
- Reuse the same discovery-backed suggestion model in catalog filters and other structured resource selectors where an exact catalog identity is available.
- Candidate follow-on uses include resource-list label-selector assistance, bounded namespace/resource filters, and known container or revision choices in operational forms. Container selectors already provided as native selects should not be replaced merely to add autocomplete.

### U5. Readable and consistent typography (Implemented)

Increase legibility while preserving the dense field-console character and desktop-first layout.

- Establish a small, centralized typography scale using relative units so browser zoom and platform text settings work predictably.
- Target approximately 14-15px for normal body and reading copy, 13-14px for standard controls and table body text, and 11-12px for secondary metadata and compact monospace values.
- Do not use 9px text for essential labels, instructions, errors, form values, table content, or status explanations. Decorative uppercase eyebrows may remain compact only when the same information is available in a readable heading or control label.
- Increase line height for descriptions, warnings, errors, Event messages, schema descriptions, and other text that can wrap. Avoid truncating essential names, messages, or validation guidance solely to preserve density.
- Increase form controls and interactive rows enough to support comfortable mouse and keyboard use, with at least a 36px control height on desktop and a 40px target on smaller screens where practical.
- Preserve the dark peat/ink, copper, moss, and dense-table visual language; improve contrast and spacing before increasing every decorative element.
- Review sidebar navigation, top-bar controls, overview cards, catalog rows, search controls, tables, Events, schema forms, operations, diagnostics, and mobile layouts together so the scale is consistent across views.
- Verify readability at normal desktop zoom, browser zoom through 200%, and the existing narrow responsive breakpoint without horizontal clipping of essential content.

### U6. Schema-generated forms for built-in resources (Implemented)

Extend the existing schema form beyond CRDs by using the Kubernetes API server's published type schema and field descriptions.

- Allow the schema endpoint and form eligibility check to accept any discovered built-in or custom-resource GVR; do not reject core or other built-in API groups solely because they are not CRDs.
- Prefer the authenticated Kubernetes API server's version-specific OpenAPI v3 schema for the requested group, version, and kind. Use the server's OpenAPI v2 document as a compatibility fallback for clusters that do not serve the required v3 document.
- Keep schema retrieval in the Go backend. The browser must receive only the bounded, normalized form model and must never fetch OpenAPI documents with kubeconfig credentials.
- Normalize built-in and custom-resource schemas into the existing bounded form model, preserving field descriptions/comments, titles, types, formats, defaults, enums, required fields, validation limits, and read-only markers where the source schema provides them.
- Use schema descriptions as the source for the field help text users recognize from `kubectl explain`; do not shell out to `kubectl` or maintain a separate hand-written form for every built-in kind.
- Respect Kubernetes identity and server-managed fields. Existing object name, namespace, resource version, UID, managed fields, and status fields must remain read-only or be omitted from editable controls according to the schema and current mutation-safety rules.
- Treat unions, recursive or preserve-unknown fields, unsupported map/array shapes, and schema traversal limits as field-level YAML fallbacks where possible. A schema-level failure must retain the complete YAML editor and state why the form is unavailable.
- Distinguish missing OpenAPI support, malformed schema, unavailable API, and RBAC failures in the form state. Do not show the current custom-resource-only message for built-in resources.
- Cache normalized schemas by kubeconfig context, revision, and exact GVR with bounded size and safe invalidation after kubeconfig or discovery changes.
- Add deterministic tests for built-in OpenAPI v3 descriptions, OpenAPI v2 fallback, version/kind matching, read-only status fields, bounded unsupported constructs, permission failures, and YAML fallback behavior.

### U7. Editable scale replica input (Implemented)

Allow operators to replace a scale value rather than being forced to edit around a coerced numeric value.

- Keep the scale field's in-progress value as a raw editable string so deleting `0` produces an empty field instead of immediately becoming numeric zero.
- Permit the field to be temporarily empty while typing. Validate on review and before apply that the final value is a whole number greater than or equal to zero.
- Preserve an explicit `0` as a valid value and do not convert blank, invalid, decimal, negative, or non-finite input into zero or `NaN`.
- Keep the existing write-mode requirement, server-side dry-run review, resource-version binding, confirmation, and post-apply refresh unchanged.
- Show an inline validation message and keep review disabled until the value is valid; allow the operator to clear and retype the complete value with normal keyboard editing and mobile numeric input behavior.
- Add focused tests covering initial zero, delete-and-type, blank validation, invalid input, and a valid transition from zero to one.

## Additional Planned Requirements

The following requirements are newly captured follow-on work and are not yet implemented.

### U8. Ingress in Configure navigation (Planned)

Make Ingress objects directly available from the Configure section of the navigation sidebar.

- Add `Ingress` to the Configure section alongside `Services` and `ConfigMaps`.
- Resolve the entry through discovery to the canonical `networking.k8s.io` Ingress group/version/resource identity rather than a guessed short route.
- Preserve the active kubeconfig context and namespace behavior when opening the Ingress resource list.
- Keep the entry and its unavailable or permission-denied state consistent with the discovery-backed resource catalog.

### U9. Clickable landing-page resource metrics (Planned)

Make the Node and Pod samples in the landing-page CPU and memory metrics panel useful navigation targets.

- Make each Node metric sample keyboard-accessible and clickable to the exact Node resource detail view.
- Make each Pod metric sample keyboard-accessible and clickable to the exact namespaced Pod resource detail view.
- Preserve the active kubeconfig context and the Pod sample's namespace when navigating.
- Use the complete discovered group/version/resource identity and do not infer a destination from an ambiguous resource name.
- Keep samples clickable when their data is stale but still visible; retain explicit loading, empty, unavailable, permission-denied, and error states when no safe destination exists.
- Provide accessible labels that identify the target kind, namespace where applicable, and name without requiring the operator to infer that a metric row is interactive.

### U10. Compute read pack for Nodes and Pods (Planned)

Extend the resource detail Read Pack with a `Compute` heading for Node and Pod resources.

- Show CPU and memory values for requests, limits, and actual usage in a compact, consistently labeled view.
- Render actual CPU and memory usage in the same display units as the corresponding request when a request value exists; fall back to the limit, capacity, or allocatable unit only when the request unit is absent, and never expose raw nanocore or mismatched memory-unit values beside the object-side quantity.
- For Pods, derive request and limit values from the resource's container specifications using standard Kubernetes resource quantities, and make it clear when values are absent or partial.
- For Nodes, show the native capacity and allocatable values for the node-side capacity fields; do not invent request or limit values when Kubernetes does not provide a safe node-level equivalent.
- Read actual Node and Pod usage from the authenticated backend metrics path already used by the landing page (`metrics.k8s.io`); do not fetch metrics documents or credentials from the browser and do not add an external metrics integration.
- Keep usage identity exact: Node usage is keyed by Node name, and Pod usage is keyed by namespace and name. Do not display usage from a same-named object in another namespace or API identity.
- Show the metrics sample time and preserve explicit available, stale, permission-denied, unavailable, empty, and partial states without hiding the object-side requests, limits, capacity, or allocatable data.
- Keep the generic YAML, Events, and related-resource views available when the Compute panel cannot be populated.
- Bound the additional metrics reads and reuse the existing metrics freshness and cancellation behavior so a resource detail page cannot hang on telemetry.

### U11. Event involved-object relationship (Planned)

Replace the Event related-resource reverse-owner action with a direct relationship to the object named by the Event's `involvedObject` field.

- Do not offer `Load owned descendants` from an Event detail page. Events are not owner controllers, so broad reverse-owner discovery provides no useful relationship and creates avoidable read errors and call-budget exhaustion.
- Always include an Event's `involvedObject` as the primary related-resource relationship when the field is present.
- Preserve and use the involved object's `apiVersion`, `kind`, `name`, `namespace`, `uid`, and other safe identity fields needed to resolve and verify the target. Resolve the target through discovery using the full API version and kind rather than guessing a resource slug.
- Make a resolved involved-object relationship clickable to the exact resource detail route, including namespace for namespaced targets. If discovery, RBAC, or object existence prevents safe resolution, show the relationship and an explicit unresolved state instead of creating an incorrect link.
- Do not require the Event target to have an owner reference, and do not replace an involved-object relationship with a same-named lookup from another API version or namespace.
- Do not implement owned-descendant loading as a reverse scan across all discovered listable resources. An unrelated custom resource must never be queried merely because it is present in discovery.
- Treat the current object's `metadata.ownerReferences` as its direct-owner data. Resolve those declared owners directly; do not perform a broad list operation to discover owners that are already named by the object.
- For descendants, query only explicitly supported controller-to-child relationships for the current kind and exact owner UID. The initial native relationship map must cover the known Kubernetes chains, including Deployment to ReplicaSet, ReplicaSet to Pod, StatefulSet to Pod, DaemonSet to Pod, Job to Pod, and CronJob to Job where the relevant APIs are available.
- Limit descendant list requests to the supported child group/version/resource identities and the safe namespace scope for the current object. Filter returned objects by their `metadata.ownerReferences` rather than treating same-name or label-matching objects as owned.
- Do not infer a generic descendant relationship for arbitrary CRDs or offer `Load owned descendants` when no supported child relationship exists. Show direct owners and other explicit relationships without performing an unrelated reverse scan.
- Keep supported descendant loading optional, bounded, and explicitly partial when a supported child API is unavailable, forbidden, times out, or reaches its limit. Report one relationship-level limitation for the affected supported child query rather than exposing a list of unrelated resource read errors.
