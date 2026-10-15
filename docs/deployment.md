# Deploying Opscope

Opscope ships as one container image that serves both the web UI and its API. It runs on your own
machine with Docker, or inside a Kubernetes cluster as a pod that shows the cluster it runs in. To
run it from source, see [CONTRIBUTING.md](../CONTRIBUTING.md).

- [Requirements](#requirements)
- [Run with Docker](#run-with-docker)
- [Run in a Kubernetes cluster](#run-in-a-kubernetes-cluster)
- [Permissions](#permissions)
- [Exposing Opscope](#exposing-opscope)
- [Configuration](#configuration)
- [Upgrading](#upgrading)

## Requirements

- A Kubernetes cluster whose API server Opscope can reach, and a kubeconfig or service account
  that can `get` and `list` the resources it shows (see [Permissions](#permissions)).
- Optional: [metrics-server](https://github.com/kubernetes-sigs/metrics-server), for CPU and memory
  usage. Without it, the usage columns and charts explain that it's missing.
- Optional: [Gateway API](https://gateway-api.sigs.k8s.io/) (`gateway.networking.k8s.io/v1`), for
  Gateways, HTTPRoutes and GatewayClasses. Without it, those pages say it isn't installed.

## Run with Docker

Each release is published to the GitHub Container Registry for `linux/amd64` and `linux/arm64`:

| Tag       | Points to                                         |
| --------- | ------------------------------------------------- |
| `v1.2.0`  | That release, never moved                         |
| `1.2`     | The latest release of that minor version          |
| `latest`  | The latest release (pre-releases are never tagged `latest`) |

Use a version tag to stay on one release.

### Add clusters in the UI

```bash
docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data ghcr.io/unnikrishnannam/opscope:latest
```

Open http://localhost:8080 and add a cluster. Clusters added in the UI are saved in the
`opscope-data` volume, so they survive restarts.

The port is published on `127.0.0.1` only, because Opscope has no login of its own (see
[Exposing Opscope](#exposing-opscope)).

### Start with a cluster from a kubeconfig file

Mount the file read-only and point `OPSCOPE_KUBECONFIG` at it:

```bash
docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data \
  -v "$HOME/.kube/lab.kubeconfig:/config/kubeconfig:ro" \
  -e OPSCOPE_KUBECONFIG=/config/kubeconfig \
  ghcr.io/unnikrishnannam/opscope:latest
```

That cluster is loaded at startup and marked "Environment" in the UI. Set `OPSCOPE_CONTEXT` to use
a context other than the file's current one. Unlike kubeconfigs added in the UI, this file may use
login commands (`exec`, as cloud CLI plugins do), provided the command exists in the image; the image
is minimal and contains no cloud CLIs, so embedded credentials are the reliable choice in a
container. `kubectl config view --minify --flatten` prints the current context with everything
embedded.

The container runs as a non-root user (uid 65532). On Linux, the mounted file must be readable by
that user.

### Reaching the cluster from a container

The container must be able to reach the cluster's API server. A `server:` address of `127.0.0.1`
or `localhost` in the kubeconfig points at the container itself, not at your machine.

**kind clusters** publish their API server on your machine's `127.0.0.1`, and that's the address in
the kubeconfig kind writes. Put Opscope on kind's Docker network and add the cluster with that
kubeconfig as it is:

```bash
docker run --rm -p 127.0.0.1:8080:8080 -v opscope-data:/data --network kind \
  ghcr.io/unnikrishnannam/opscope:latest
```

or connect a container that's already running:

```bash
docker network connect kind <opscope-container>
```

kind creates the `kind` network with its first cluster and puts every cluster on it, so one
container reaches all of them. Opscope dials the address in the kubeconfig first. When nothing
answers there and the cluster or context is named `kind-<name>`, as kind names them, it connects to
the cluster's node by name on kind's network, at `<name>-control-plane:6443`. The kubeconfig is
saved unchanged, and the API server's certificate is still checked against the address in it, which
kind's certificate lists. The Clusters page shows where such a cluster was reached ("reached at
dev-control-plane:6443 on kind's network"), and if Opscope isn't on kind's network, the cluster's
error says so.

Outside a container, kind's address works and Opscope uses it directly.

If you renamed both the cluster and the context, Opscope can't tell the cluster is a kind cluster.
Use the kubeconfig kind writes for its own network instead:

```bash
kind get kubeconfig --internal --name <cluster>
```

**With Docker Compose,** [`compose.yaml`](../compose.yaml) in the repository runs Opscope on kind's
network, with the same port and volume as the examples above:

```bash
docker compose up -d
```

Create a kind cluster first: until kind has created its network, Compose stops with "network kind
declared as external, but could not be found". Set `OPSCOPE_PORT` to publish another port, and
`OPSCOPE_IMAGE` to run another image, such as a local build (`opscope:dev`).

### Build the image yourself

From a clone of the repository:

```bash
make docker-build
```

This builds `opscope:dev`. `make docker-run` and `make docker-run-env KUBECONFIG_FILE=path` run it
with the same settings as above. Pass `VERSION=v1.2.0` to either to set the image tag and the version
Opscope reports, and `NETWORK=kind` to put the container on kind's network.

## Run in a Kubernetes cluster

The manifests in [`deploy/kubernetes/`](../deploy/kubernetes/) run Opscope as a pod that shows the
cluster it runs in, using its own service account:

| File                  | Creates                                                                                    |
| --------------------- | ------------------------------------------------------------------------------------------ |
| `opscope.yaml`        | The `opscope` namespace, a service account, a read-only ClusterRole and its binding, the Deployment and a ClusterIP Service |
| `secrets-access.yaml` | Optional: a second ClusterRole that allows reading Secrets (see [Secret access](#secret-access)) |

The manifests in each release tag use that release's published image, so you can apply them
straight from GitHub:

```bash
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.2.0/deploy/kubernetes/opscope.yaml
```

```bash
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.2.0/deploy/kubernetes/secrets-access.yaml
```

or, from a clone of the repository, `kubectl apply -f deploy/kubernetes/`. Then open Opscope through
a port-forward:

```bash
kubectl -n opscope port-forward svc/opscope 8080:80
```

Open http://localhost:8080. The cluster appears as "this cluster"; set `OPSCOPE_CLUSTER_NAME` in the
Deployment to name it.

### How the pod runs

- As a non-root user, with a read-only root filesystem, no Linux capabilities and the default
  seccomp profile. It writes only to `/data`.
- Liveness and readiness probes use `/api/health`.
- One replica: usage history is kept in memory, so one copy keeps it consistent.
- Requests 20m CPU and 48 MiB of memory, with a 256 MiB memory limit.
- On `SIGTERM`, Opscope stops accepting connections and gives running requests up to 10 seconds to
  finish; open log streams are closed after that.

### Keeping clusters added in the UI

Other clusters can be added in the UI of an in-cluster Opscope too. They are saved in `/data`, which
the manifest mounts as an `emptyDir`, so they are lost when the pod is replaced. To keep them, use a
PersistentVolumeClaim:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: opscope-data
  namespace: opscope
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 64Mi # saved clusters are a few KB each; some storage classes round this up
```

and replace the `data` volume in the Deployment:

```yaml
volumes:
  - name: data
    persistentVolumeClaim:
      claimName: opscope-data
```

The pod's `fsGroup` makes the volume writable for Opscope's user.

### Using an image of your own

Set `image:` in `opscope.yaml` to your image. For a kind cluster, load a local build straight into
its nodes:

```bash
make docker-build && kind load docker-image opscope:dev --name <cluster>
```

## Permissions

Opscope only ever uses `get` and `list`. It never creates, changes or deletes anything, and it polls
rather than watching. For the topology and the diagnosis it also reads each cluster in the
background; see [Background snapshots](#background-snapshots).

| API group                   | Resources                                                                  | Used for               |
| --------------------------- | -------------------------------------------------------------------------- | ---------------------- |
| core (`""`)                 | namespaces, nodes, pods, services, configmaps, events                      | Lists and detail pages |
| core (`""`)                 | persistentvolumeclaims, persistentvolumes, serviceaccounts, resourcequotas | Topology and diagnosis |
| core (`""`)                 | pods/log (`get`)                                                           | The Logs tab           |
| `apps`                      | deployments, statefulsets, daemonsets                                      | Workloads              |
| `apps`                      | replicasets                                                                | Topology and diagnosis |
| `batch`                     | jobs, cronjobs                                                             | Workloads              |
| `discovery.k8s.io`          | endpointslices                                                             | Topology and diagnosis |
| `networking.k8s.io`         | ingresses                                                                  | Networking             |
| `networking.k8s.io`         | ingressclasses                                                             | Topology and diagnosis |
| `storage.k8s.io`            | storageclasses                                                             | Topology and diagnosis |
| `autoscaling`               | horizontalpodautoscalers                                                   | Topology and diagnosis |
| `policy`                    | poddisruptionbudgets                                                       | Topology and diagnosis |
| `gateway.networking.k8s.io` | gateways, httproutes, gatewayclasses, referencegrants                      | Gateway API (optional) |
| `metrics.k8s.io`            | nodes, pods                                                                | CPU and memory usage   |
| core (`""`)                 | secrets (`secrets-access.yaml` only)                                       | Secrets pages, Reveal  |

The same applies to a kubeconfig used from outside the cluster: Opscope can show only what that
user is allowed to read. Anything it can't read is reported on the page that needs it, and the rest
keeps working.

### Secret access

Secrets are listed by key name only. A value is fetched only when someone chooses "Reveal" on one
key, is sent with `Cache-Control: no-store`, and never appears in the YAML view.

**Granting Secret access is a deliberate choice.** With `secrets-access.yaml` applied, the Opscope
pod can read every Secret in the cluster, and so can anyone who can reach Opscope. To remove that
access:

```bash
kubectl delete -f deploy/kubernetes/secrets-access.yaml
```

The Secrets pages then say that this user isn't allowed to read them, and the diagnosis says it
can't check a Secret a pod needs rather than calling it missing. Even with the role, the topology
and the diagnosis list Secrets by their metadata only: their values never reach them.

## Exposing Opscope

**Opscope has no login.** Anyone who can open it can read everything it can read, in every cluster
it knows, and can add or remove clusters in its UI. That is why:

- the Docker examples publish the port on `127.0.0.1` only;
- the Kubernetes Service is `ClusterIP`, reached with `kubectl port-forward`.

Don't expose Opscope with an Ingress, a Gateway or a `LoadBalancer` Service unless something in
front of it handles authentication, such as an authenticating reverse proxy
([oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/) or your platform's equivalent).

## Configuration

Opscope is configured with environment variables:

| Variable                    | Default            | Effect                                                                             |
| --------------------------- | ------------------ | ---------------------------------------------------------------------------------- |
| `PORT`                      | `8080`             | Port the server listens on                                                         |
| `OPSCOPE_KUBECONFIG`        | (none)             | Kubeconfig file for a cluster loaded at startup                                    |
| `OPSCOPE_CONTEXT`           | current context    | Which context of that file to use                                                  |
| `OPSCOPE_IN_CLUSTER`        | (off)              | `true`: use the pod's service account, when running in a pod                       |
| `OPSCOPE_CLUSTER_NAME`      | context name       | Display name for the environment or in-cluster cluster                             |
| `DATA_DIR`                  | `../data`          | Where clusters added in the UI are saved (`/data` in the image)                    |
| `STATIC_DIR`                | `../frontend/dist` | Folder with the built web UI (`/app/web` in the image)                             |
| `OPSCOPE_SNAPSHOT_INTERVAL` | `30s`              | How often each cluster is read in the background (`10s` or more), or `0` for never |
| `OPSCOPE_LOG_LEVEL`         | `info`             | `debug`, `info`, `warn` or `error`; `debug` logs each background snapshot          |

Opscope never reads `~/.kube/config` or `KUBECONFIG` on its own, so a cluster is never picked up by
accident. If `OPSCOPE_KUBECONFIG` or `OPSCOPE_IN_CLUSTER` is set but can't be used, Opscope stops
with an error rather than starting without that cluster.

Clusters added in the UI are saved one file per cluster, readable only by Opscope's user, and contain
only the chosen context with its cluster and credentials.

### Background snapshots

For the topology and the diagnosis, Opscope reads each cluster whole: one `list` call per kind it
knows (27 of them), more for kinds with over 500 objects. It does this every 30 seconds whether
anyone is looking or not, so the cluster's map is ready when you open it. While someone has a page
open, it reads up to every 10 seconds, and the background reading waits until the page's reading is
30 seconds old. A cluster that doesn't answer is tried again after one minute, then two, four, and
then every five minutes, until it answers. Only the latest snapshot of each cluster is kept, in
memory.

On a cluster of about 350 objects that's some 50 small `list` calls a minute, each snapshot taking
around 20 ms to read, and Opscope uses under 50 MB of memory.

Set `OPSCOPE_SNAPSHOT_INTERVAL=0` to read clusters only while a page asks for them, or a longer
interval (`2m`) for a cluster you'd rather read less often.

## Upgrading

- **Docker:** pull and run the new tag. Saved clusters in the `opscope-data` volume are kept.
- **Kubernetes:** apply the manifests from the new release's tag, or change the image tag in the
  Deployment.

Usage history is kept in memory and starts empty after a restart. Each release's notes on
[GitHub](https://github.com/unnikrishnanNam/opscope/releases) say whether anything else changes.
