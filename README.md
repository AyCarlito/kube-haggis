# KubeHaggis

KubeHaggis is a local Kubernetes console for resources, cluster health, and guarded operations.

## Requirements

- Go 1.26 or newer.
- Node.js and npm to build the frontend.
- Access to a Kubernetes cluster and a valid kubeconfig.

KubeHaggis uses `KUBECONFIG` when it is set and otherwise falls back to `~/.kube/config`. It starts from the kubeconfig `current-context` and keeps kubeconfig credentials in the Go backend; they are not sent to the browser.

If your kubeconfig uses an external authentication plugin such as an AWS, GCP, Azure, or OIDC helper, that helper must also be installed and available in the environment where KubeHaggis runs.

## Build And Run

From the repository root:

```sh
npm --prefix web ci
npm --prefix web run build
go run ./cmd/kube-haggis
```

Open <http://127.0.0.1:8080>.

The server listens on `127.0.0.1:8080` by default. The frontend must be built before starting the Go server because the server serves the generated files from `web/dist`.

## First Use

- Choose a kubeconfig context from the context selector if you do not want to use the startup `current-context`.
- Choose a namespace, or leave the scope as `All namespaces`.
- KubeHaggis starts in read-only mode.
- Enable write mode for the selected context only when you need to create, edit, scale, restart, or delete resources.
- Kubernetes RBAC remains the authorization authority. Write mode does not grant permissions.
- Mutations use review, resource-version checks, and Kubernetes server-side dry-run where supported.
- Secret values are masked by default.

## Optional Metrics

The overview and resource Compute panels use the Kubernetes `metrics.k8s.io` API. The rest of the console remains usable when a metrics server is not installed or the current identity cannot read metrics.

## Local History

Redacted mutation history is stored in SQLite under the platform configuration directory. Choose another path with:

```sh
go run ./cmd/kube-haggis -audit-db /path/to/kube-haggis-audit.db
```

Useful startup flags include:

```text
-kubeconfig /path/to/config   Use an explicit kubeconfig file
-addr 127.0.0.1:8080          Change the local listen address
-audit-db /path/to/audit.db   Choose the SQLite mutation-history path
```

Keep the listen address on localhost unless you have deliberately configured network access and understand that the application has no separate login layer.

## Frontend Development

To run the Vite development server, start KubeHaggis normally first, then run:

```sh
npm --prefix web run dev
```

The Vite server proxies API requests to the local Go server.
