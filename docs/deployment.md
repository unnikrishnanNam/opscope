# Deploying Opscope

This guide is for running Opscope for a team: installing it, choosing what it may read, giving
people access, and keeping it up to date. To try Opscope on your own machine first, see
[Running Opscope on your machine](local.md). To work on Opscope's code, see
[CONTRIBUTING.md](../CONTRIBUTING.md).

Opscope ships as one container image that serves both the web UI and its API.

- [Before you start](#before-you-start)
- [Install in a cluster](#install-in-a-cluster)
- [Permissions](#permissions)
- [Giving people access](#giving-people-access)
- [Keeping clusters added in the UI](#keeping-clusters-added-in-the-ui)
- [Run with Docker on a server](#run-with-docker-on-a-server)
- [Configuration](#configuration)
- [Upgrading](#upgrading)
- [Uninstalling](#uninstalling)

## Before you start

You need:

- A Kubernetes cluster whose API server Opscope can reach, and a service account or kubeconfig that
  can `get` and `list` the resources it shows (see [Permissions](#permissions)).
- Optional: [metrics-server](https://github.com/kubernetes-sigs/metrics-server), for CPU and memory
  usage. Without it, the usage columns and charts explain that it's missing.
- Optional: [Gateway API](https://gateway-api.sigs.k8s.io/) (`gateway.networking.k8s.io/v1`), for
  Gateways, HTTPRoutes and GatewayClasses. Without it, those pages say it isn't installed.

Then choose where Opscope runs:

- **In the cluster it shows** (recommended). Opscope runs as a pod with its own read-only service
  account, so there are no kubeconfigs to hand out, and people reach it through a port-forward or an
  authenticating proxy. See [Install in a cluster](#install-in-a-cluster).
- **With Docker on a server**, when Opscope should show clusters it doesn't run in, or several
  clusters from one place. It reads them with kubeconfigs you give it. See
  [Run with Docker on a server](#run-with-docker-on-a-server).

Either way, more clusters can be added in the UI later.

## Install in a cluster

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
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.3.0/deploy/kubernetes/opscope.yaml
```

From a clone of the repository, apply `deploy/kubernetes/opscope.yaml` the same way. Apply the file,
not the whole folder: the folder also holds the two optional roles.

### Check that it's working

Wait for the pod to be ready:

```bash
kubectl -n opscope rollout status deployment/opscope
```

Then open Opscope through a port-forward:

```bash
kubectl -n opscope port-forward svc/opscope 8080:80
```

Open http://localhost:8080. The cluster appears as "this cluster" (set `OPSCOPE_CLUSTER_NAME` in the
Deployment to name it), and its overview shows its nodes and workloads. If the pod doesn't become
ready, its log says why:

```bash
kubectl -n opscope logs deployment/opscope
```

### How the pod runs

- As a non-root user, with a read-only root filesystem, no Linux capabilities and the default
  seccomp profile. It writes only to `/data`.
- Liveness and readiness probes use `/api/health`.
- One replica: usage history is kept in memory, so one copy keeps it consistent.
- Requests 20m CPU and 48 MiB of memory, with a 256 MiB memory limit.
- On `SIGTERM`, Opscope stops accepting connections and gives running requests up to 10 seconds to
  finish; open log streams are closed after that.

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

`opscope.yaml` grants everything above except the last two rows. The same applies to a kubeconfig
used from outside the cluster: Opscope can show only what that user is allowed to read. Anything it
can't read is reported on the page that needs it, and the rest keeps working.

### Secret access

Secrets are listed by key name only. A value is fetched only when someone chooses "Reveal" on one
key, is sent with `Cache-Control: no-store`, and never appears in the YAML view.

**Granting Secret access is a deliberate choice.** With `secrets-access.yaml` applied, the Opscope
pod can read every Secret in the cluster, and so can anyone who can reach Opscope. To grant it:

```bash
kubectl apply -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.3.0/deploy/kubernetes/secrets-access.yaml
```

To remove it again:

```bash
kubectl delete -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.3.0/deploy/kubernetes/secrets-access.yaml
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
2. Grant the permission. In a cluster, apply `volume-usage.yaml` from the same release as
   `opscope.yaml`. With a kubeconfig, the user's own permissions decide; cluster admins usually
   have it.

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

To turn it off, remove the setting, and delete `volume-usage.yaml` the same way you applied it.

## Giving people access

**Opscope has no login.** Anyone who can open it can read everything it can read, in every cluster
it knows, and can add or remove clusters in its UI. That is why:

- the Docker examples publish the port on `127.0.0.1` only;
- the Kubernetes Service is `ClusterIP`, reached with `kubectl port-forward`.

There are two safe ways to let people in:

- **Port-forward.** Each person runs `kubectl -n opscope port-forward svc/opscope 8080:80` with
  their own kubectl access, so only people allowed to port-forward in the `opscope` namespace can
  reach it.
- **An authenticating proxy** in front of Opscope, such as
  [oauth2-proxy](https://oauth2-proxy.github.io/oauth2-proxy/) or your platform's equivalent, which
  signs people in before passing requests on.

Don't expose Opscope with an Ingress, a Gateway or a `LoadBalancer` Service without such a proxy.

## Keeping clusters added in the UI

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

## Run with Docker on a server

Each release is published to the GitHub Container Registry for `linux/amd64` and `linux/arm64`:

| Tag       | Points to                                         |
| --------- | ------------------------------------------------- |
| `v1.3.0`  | That release, never moved                         |
| `1.3`     | The latest release of that minor version          |
| `latest`  | The latest release (pre-releases are never tagged `latest`) |

On a server, pin a version tag, so that a restart never upgrades Opscope by surprise. The commands
are the same as on your own machine: see
[Running Opscope on your machine](local.md#run-it-with-docker) for adding clusters in the UI, giving
Opscope a kubeconfig file at startup, and reaching a cluster from the container. On a server:

- Keep the port on `127.0.0.1`, and put an authenticating proxy on the same server in front of it
  (see [Giving people access](#giving-people-access)).
- Keep the `opscope-data` volume: it holds the clusters added in the UI.
- Add `--restart unless-stopped` instead of `--rm`, so Opscope comes back after a reboot.

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

- **In a cluster:** apply the manifests from the new release's tag (including any optional roles
  you use), or change the image tag in the Deployment.
- **Docker:** pull and run the new tag. Saved clusters in the `opscope-data` volume are kept.

Usage history is kept in memory and starts empty after a restart. Each release's notes on
[GitHub](https://github.com/unnikrishnanNam/opscope/releases) say whether anything else changes.

## Uninstalling

**In a cluster,** delete the optional roles you applied, then `opscope.yaml`:

```bash
kubectl delete -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.3.0/deploy/kubernetes/secrets-access.yaml --ignore-not-found
```

```bash
kubectl delete -f https://raw.githubusercontent.com/unnikrishnanNam/opscope/v1.3.0/deploy/kubernetes/opscope.yaml
```

Do the same for `volume-usage.yaml` if you applied it. Deleting `opscope.yaml` deletes the
`opscope` namespace, and with it a PersistentVolumeClaim for saved clusters, if you added one.

**With Docker,** stop the container, then remove the clusters it saved:

```bash
docker volume rm opscope-data
```
