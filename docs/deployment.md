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

**kind clusters** publish their API server on your machine's `127.0.0.1`. Put Opscope on kind's
Docker network instead, and use the kubeconfig kind writes for that network:

```bash
kind get kubeconfig --internal --name <cluster> > kind.internal.kubeconfig
```

Then add `--network kind` to `docker run`, or connect a running container:

```bash
docker network connect kind <opscope-container>
```

### Build the image yourself

From a clone of the repository:

```bash
make docker-build
```

This builds `opscope:dev`. `make docker-run` and `make docker-run-env KUBECONFIG_FILE=path` run it
with the same settings as above. Pass `VERSION=v1.2.0` to either to set the image tag and the version
Opscope reports.

## Run in a Kubernetes cluster

The manifests in [`deploy/kubernetes/`](../deploy/kubernetes/) run Opscope as a pod that shows the
cluster it runs in, using its own service account:

| File                  | Creates                                                                                    |
| --------------------- | ------------------------------------------------------------------------------------------ |
| `opscope.yaml`        | The `opscope` namespace, a service account, a read-only ClusterRole and its binding, the Deployment and a ClusterIP Service |
| `secrets-access.yaml` | Optional: a second ClusterRole that allows reading Secrets (see [Secret access](#secret-access)) |
| `volume-usage.yaml`   | Optional: a ClusterRole that allows reading volume usage from the kubelets (see [Volume usage](#volume-usage)) |

The manifests in each release tag use that release's published image, so you can apply them
straight from GitHub:

```bash
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.2.0/deploy/kubernetes/opscope.yaml
```

```bash
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.2.0/deploy/kubernetes/secrets-access.yaml
```

or, from a clone of the repository, `kubectl apply -f deploy/kubernetes/opscope.yaml` (applying the
whole folder also applies the optional files). Then open Opscope through a port-forward:

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
rather than watching.

| API group                   | Resources                                             | Used for               |
| --------------------------- | ----------------------------------------------------- | ---------------------- |
| core (`""`)                 | namespaces, nodes, pods, services, configmaps, events | Lists and detail pages |
| core (`""`)                 | pods/log (`get`)                                      | The Logs tab           |
| `apps`                      | deployments, statefulsets, daemonsets                 | Workloads              |
| `batch`                     | jobs, cronjobs                                        | Workloads              |
| `networking.k8s.io`         | ingresses                                             | Networking             |
| core (`""`)                 | persistentvolumeclaims, persistentvolumes             | Storage                |
| `storage.k8s.io`            | storageclasses                                        | Storage                |
| `gateway.networking.k8s.io` | gateways, httproutes, gatewayclasses                  | Gateway API (optional) |
| `metrics.k8s.io`            | nodes, pods                                           | CPU and memory usage   |
| core (`""`)                 | secrets (`secrets-access.yaml` only)                  | Secrets pages, Reveal  |
| core (`""`)                 | nodes/proxy (`get`, `volume-usage.yaml` only)         | Volume usage (opt-in)  |

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

The Secrets pages then say that this user isn't allowed to read them.

### Volume usage

Kubernetes doesn't record how full a volume is; only the kubelet on the node where it's mounted
knows. With volume usage on, the PersistentVolumeClaims list gets a "Used" column, claim and volume
pages show used and free space and inodes, and the overview counts claims that are 90% full or
more. It's off by default, because of the permission it needs.

Opscope reads each node's stats through the API server (`/api/v1/nodes/{node}/proxy/stats/summary`),
which needs `get` on `nodes/proxy`. Kubernetes has no narrower permission for this route:
`nodes/proxy` reaches the kubelet's whole API, including the parts that run commands in containers.
Opscope only ever asks for the stats summary, but the permission itself allows more, so, like
Secret access, it's worth turning on deliberately. Two steps turn it on:

1. Set `OPSCOPE_VOLUME_USAGE=true` (`-e OPSCOPE_VOLUME_USAGE=true` with Docker, or the commented
   lines in `opscope.yaml`). Without it, Opscope never calls the node proxy, whatever its
   credentials allow.
2. Grant the permission. In a cluster, apply `volume-usage.yaml`. With a kubeconfig, the user's own
   permissions decide; cluster admins usually have it.

If the setting is on but the permission is missing, the pages say which permission is needed, and
everything else keeps working.

A few things to know:

- Usage is only known for claims mounted by a running pod; the kubelet measures nothing else.
- The kubelet measures CSI volumes (most cloud and network storage) and `local` volumes, but not
  hostPath ones. Local clusters often provision claims as hostPath folders (the local-path
  provisioner on kind and k3s, for example); those claims show "Not measured".
- The kubelet measures volumes about once a minute. Opscope asks only the nodes that run pods with
  claims, and keeps each node's answer for 30 seconds.
- Raw block volumes (`volumeMode: Block`) have no filesystem to measure.

To turn it off, remove the setting and the permission:

```bash
kubectl delete -f deploy/kubernetes/volume-usage.yaml
```

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

| Variable               | Default            | Effect                                                            |
| ---------------------- | ------------------ | ----------------------------------------------------------------- |
| `PORT`                 | `8080`             | Port the server listens on                                        |
| `OPSCOPE_KUBECONFIG`   | (none)             | Kubeconfig file for a cluster loaded at startup                   |
| `OPSCOPE_CONTEXT`      | current context    | Which context of that file to use                                 |
| `OPSCOPE_IN_CLUSTER`   | (off)              | `true`: use the pod's service account, when running in a pod      |
| `OPSCOPE_CLUSTER_NAME` | context name       | Display name for the environment or in-cluster cluster            |
| `OPSCOPE_VOLUME_USAGE` | (off)              | `true`: show how full volumes are (see [Volume usage](#volume-usage)) |
| `DATA_DIR`             | `../data`          | Where clusters added in the UI are saved (`/data` in the image)   |
| `STATIC_DIR`           | `../frontend/dist` | Folder with the built web UI (`/app/web` in the image)            |

Opscope never reads `~/.kube/config` or `KUBECONFIG` on its own, so a cluster is never picked up by
accident. If `OPSCOPE_KUBECONFIG` or `OPSCOPE_IN_CLUSTER` is set but can't be used, Opscope stops
with an error rather than starting without that cluster.

Clusters added in the UI are saved one file per cluster, readable only by Opscope's user, and contain
only the chosen context with its cluster and credentials.

## Upgrading

- **Docker:** pull and run the new tag. Saved clusters in the `opscope-data` volume are kept.
- **Kubernetes:** apply the manifests from the new release's tag, or change the image tag in the
  Deployment.

Usage history is kept in memory and starts empty after a restart. Each release's notes on
[GitHub](https://github.com/unnikrishnanNam/opscope/releases) say whether anything else changes.
