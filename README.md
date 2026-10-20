# KubeHaggis

Kubernetes-native cluster console for local kubeconfig contexts.

## Run locally

Build the browser application, then start the localhost server:

```sh
cd web
npm install
npm run build
cd ..
go run ./cmd/kube-haggis
```

Open `http://127.0.0.1:8080`. KubeHaggis follows standard Kubernetes kubeconfig loading: it uses `KUBECONFIG` when set and otherwise falls back to `~/.kube/config`.

Redacted mutation history is stored in the platform configuration directory. Use `-audit-db` to choose another SQLite path.

The frontend can also run independently during development:

```sh
cd web
npm run dev
```

The Vite server proxies Kubernetes API requests to the local Go server.
