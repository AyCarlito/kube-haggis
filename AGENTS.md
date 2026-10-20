# KubeHaggis Agent Guide

## Project

KubeHaggis is a personal, internal-only Kubernetes management console. It runs as a local web server, reads the host kubeconfig, and does not run inside a cluster.

The product name is `KubeHaggis`. Use `kube-haggis` for command names, paths, and package names where kebab-case is required. The repository-provided `haggis.svg` is the product mark and must be used by the UI. Do not replace, regenerate, or remove it.

The current product scope and implementation status are maintained in `docs/requirements.md`.

## Architecture

- `cmd/kube-haggis/` contains the local Go server entrypoint.
- `internal/kube/` contains kubeconfig loading, Kubernetes discovery, dynamic resource access, watches, mutations, and native operational clients.
- `internal/server/` contains the local HTTP API and frontend serving layer.
- `internal/audit/` contains redacted SQLite mutation history.
- `web/src/` contains the React and TypeScript browser application.
- `web/dist/` is generated frontend output and should not be edited manually.
- Use Kubernetes dynamic and discovery clients for built-in resources and CRDs. Do not add a typed backend implementation for every resource kind.

## Product Constraints

- Use standard `KUBECONFIG` loading and fall back to `~/.kube/config`.
- Start from kubeconfig `current-context` on application startup.
- Keep kubeconfig contents and credentials in the Go backend; never send them to the browser.
- Bind to `127.0.0.1` by default.
- Do not add application-level login in the initial release.
- Use Kubernetes RBAC as the authorization authority.
- Keep Helm, Argo CD, Flux, cloud-provider operations, and cluster lifecycle management out of scope.
- Preserve deep-linkable context, namespace, resource-list, and resource-detail URLs.
- Preserve per-context namespace persistence.

## Mutation Safety

- Start in read-only mode and require explicit per-context write mode.
- Client-side writes use full-object Kubernetes updates with resource-version concurrency. Do not reintroduce server-side apply patches without an explicit requirements change.
- Keep dry-run review before writes where Kubernetes supports it.
- Do not submit `metadata.managedFields`; strip it from editor output and mutation payloads.
- Mask Secret values by default and require an explicit reveal action.
- Never put Secret values in logs, audit history, error messages, or generated diffs.
- Use stronger confirmation for deletion, eviction, drain, namespace, and other destructive actions.
- Record redacted mutation outcomes in the local SQLite audit store.

## UI Direction

- Preserve the KubeHaggis field-console visual language: dark peat/ink surfaces, copper accents, moss status colors, dense operator tables, and the haggis mark.
- Keep the UI desktop-first and responsive down to smaller screens.
- Prefer explicit loading, empty, disconnected, and permission-denied states over blank panels.
- Use a neutral bootstrap loading state while kubeconfig and context data initialize. Do not show the missing-kubeconfig state until loading has completed.
- Keep resource lists discovery-driven and generic, with targeted actions layered on top.
- Keep YAML available as the complete fallback for every resource and CRD.

## Development Commands

Run backend tests and static analysis from the repository root:

```sh
go test ./...
go vet ./...
```

Build the frontend from `web/`:

```sh
npm install
npm run build
```

Run the application locally after building the frontend:

```sh
go run ./cmd/kube-haggis
```

The default server is `http://127.0.0.1:8080`. Do not start a long-lived or background KubeHaggis server during development unless the user explicitly asks. Short-lived endpoint checks are acceptable when needed for verification.

## Code Practices

- Use `apply_patch` for manual source and documentation edits.
- Document every type and function added to a source file in that same file.
- Keep backend resource behavior generic and extensible for CRDs.
- Add focused tests for resource aggregation, API handlers, mutation safety, and audit behavior.
- Prefer fake Kubernetes clients or isolated handler tests when no live kubeconfig or cluster is available.
- Do not inspect or modify other branches or repository history unless the user explicitly asks.
- Do not revert user changes or unrelated worktree changes.
- Keep generated dependencies and frontend build output out of manual edits.
