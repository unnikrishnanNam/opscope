# Test scenarios

Manifests that set up cases a healthy cluster doesn't have, to check Opscope's pages against. Run
them in a throwaway [kind](https://kind.sigs.k8s.io/) cluster with its own kubeconfig, never in a
cluster you care about.

## Workloads

`workloads.yaml` makes pods that fail in the common ways, next to a healthy Deployment, to check
that the pages show each failure and keep up with it without a reload:

```bash
kind create cluster --name opscope-workloads --kubeconfig /tmp/opscope-workloads.kubeconfig
export KUBECONFIG=/tmp/opscope-workloads.kubeconfig
until kubectl apply -f test/scenarios/workloads.yaml; do sleep 2; done
```

Run Opscope with `OPSCOPE_KUBECONFIG=/tmp/opscope-workloads.kubeconfig` and look at the
`workloads-test` namespace. The failures settle within a minute:

| Object                       | What the pages should show                                                                                                                                                    |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deployment and Service `web` | 2 of 2 ready: the healthy one to compare with                                                                                                                                 |
| Pod `crashloop-…`            | Error, then CrashLoopBackOff, restarts rising. Last exit "Error (exit 1)"; its previous logs say `cannot read /etc/app/config.yaml`. Deployment `crashloop`: 0 of 1 available |
| Pod `oom`                    | OOMKilled, then CrashLoopBackOff, restarts rising. Last exit "OOMKilled (exit 137)"                                                                                           |
| Pod `bad-image`              | ErrImagePull, then ImagePullBackOff, no restarts. A warning event: failed to pull the image                                                                                   |
| Service `no-endpoints`       | Listed like any Service, its selector `app=web-frontend` on the detail page. Nothing says yet that it matches no pods: that's for a later release's issues feed               |
| Overview                     | Pods by status: 2 Running and one of each failure. Deployments: 1 of 2 not ready. Warnings: BackOff and Failed                                                                |

**Live updates:** leave the Pods page open. Each restart and status change shows within about a
second, without a reload. The restarts slow down as Kubernetes waits longer each time, up to once
every five minutes.

### Clean up

```bash
kind delete cluster --name opscope-workloads
```

## Storage

```bash
kind create cluster --name opscope-storage --kubeconfig /tmp/opscope-storage.kubeconfig
export KUBECONFIG=/tmp/opscope-storage.kubeconfig
until kubectl apply -f test/scenarios/storage.yaml; do sleep 2; done
```

On a new cluster the first try can be refused for the two pods: the namespace's `default` service
account is created a moment after the namespace, and a pod can't be made before it exists. The
loop applies again until it's accepted.

Once the pods are running, two deletions finish the setup:

```bash
kubectl -n storage-test delete pvc retained-claim
kubectl -n storage-test delete pvc in-use --wait=false
```

Then run Opscope with `OPSCOPE_KUBECONFIG=/tmp/opscope-storage.kubeconfig` and look at the
`storage-test` namespace:

| Object                        | What the pages should show                                                  |
| ----------------------------- | --------------------------------------------------------------------------- |
| StatefulSet `db`              | Its claim template, and the claims `data-db-0` and `data-db-1` made from it |
| Pod `worker`                  | Volumes: a generic ephemeral claim, a ConfigMap, a Secret, a memory emptyDir |
| Claim `unused`                | Pending: kind's StorageClass waits for a pod that uses the claim            |
| Claim `missing-class`         | Pending, with StorageClass `does-not-exist (not found)` and a warning event |
| Volume `retained`             | Released, its claim `(no longer bound)`, StorageClass `manual (not found)`  |
| Claim `in-use`                | Terminating, still used by the pod `holder`                                 |
| Overview                      | Claims not bound, a volume released                                         |

### Volume usage

The kubelet doesn't measure hostPath volumes, which is what kind's local-path StorageClass makes,
so those claims read "Not measured". For a claim with numbers, `storage-usage.yaml` uses a `local`
volume on a small tmpfs inside the kind node, filled to about 92%. Mount the tmpfs first:

```bash
docker exec opscope-storage-control-plane sh -c \
  'mkdir -p /mnt/small && mount -t tmpfs -o size=64m,nr_inodes=2000 tmpfs /mnt/small'
kubectl apply -f test/scenarios/storage-usage.yaml
```

Run Opscope with `OPSCOPE_VOLUME_USAGE=true` as well. Within a minute or two (the kubelet measures
about once a minute), the claim `nearly-full` shows a red bar at about 92%, and the overview counts
it as nearly full.

**Without the permission:** connect as Opscope's own service account, which `opscope.yaml` sets up
without `nodes/proxy`:

```bash
kubectl apply -f deploy/kubernetes/opscope.yaml
kubectl config set-credentials opscope-sa --token="$(kubectl -n opscope create token opscope --duration=24h)"
kubectl config set-context opscope-sa --cluster=kind-opscope-storage --user=opscope-sa
```

With `OPSCOPE_CONTEXT=opscope-sa`, the storage pages say that `nodes/proxy` is needed and everything
else works. After `kubectl apply -f deploy/kubernetes/volume-usage.yaml`, the numbers appear within
30 seconds.

**A node that can't be read:** stop the kubelet for a moment. The API server keeps running, but
can't reach the kubelet:

```bash
docker exec opscope-storage-control-plane systemctl stop kubelet
docker exec opscope-storage-control-plane systemctl start kubelet
```

While it's stopped, the claims list says which node couldn't be read, and its claims show a dash
with the error on hover.

### Clean up

```bash
kind delete cluster --name opscope-storage
```

## Permissions

`permissions.yaml` adds two users who may read less than `opscope.yaml` grants, so you can check how
Opscope reads each kind with what it's allowed:

```bash
kind create cluster --name opscope-live --kubeconfig /tmp/opscope-live.kubeconfig
export KUBECONFIG=/tmp/opscope-live.kubeconfig
until kubectl apply -f test/scenarios/permissions.yaml; do sleep 2; done
```

Make a kubeconfig for each user, with a token for its service account:

```bash
for user in no-watch team-a; do
  cp /tmp/opscope-live.kubeconfig /tmp/opscope-$user.kubeconfig
  KUBECONFIG=/tmp/opscope-$user.kubeconfig kubectl config set-credentials $user \
    --token="$(kubectl -n opscope-users create token $user --duration=24h)"
  KUBECONFIG=/tmp/opscope-$user.kubeconfig kubectl config set-context $user --cluster=kind-opscope-live --user=$user
  KUBECONFIG=/tmp/opscope-$user.kubeconfig kubectl config use-context $user
done
```

Run Opscope with `OPSCOPE_KUBECONFIG=/tmp/opscope-live.kubeconfig` and add the two kubeconfigs in the
UI. `GET /api/clusters/{id}/live` shows how each kind is read:

| Cluster    | What it should show                                                                    |
| ---------- | -------------------------------------------------------------------------------------- |
| The admin  | Every kind `watch`, Gateway API `not_installed`; state `live`                          |
| `no-watch` | The kinds it may list `poll`, Secrets `direct`; state `partial`. A new pod shows up within 30 seconds |
| `team-a`   | Namespaced kinds `direct`, cluster-wide kinds `forbidden`. Pods in `team-a` list; across all namespaces, the page says it isn't allowed. Typing `team-a` in the namespace picker offers it; Topology, with no namespace, asks for one by name |

Gateway API kinds show as `direct` or `forbidden` for the two users, not `not_installed`: the API
server checks the permission before it checks that the kind exists.

**A permission granted later:** give `no-watch` the `watch` verb on the core kinds. Within five
minutes, without a restart, those kinds change to `watch`:

```bash
kubectl patch clusterrole opscope-test-no-watch --type=json \
  -p '[{"op":"replace","path":"/rules/0/verbs","value":["get","list","watch"]}]'
```

**A cluster that stops answering:** pause the kind node for half a minute.

```bash
docker pause opscope-live-control-plane
docker unpause opscope-live-control-plane
```

Within about 15 seconds the state is `unreachable` and pages show the cluster's error; the log has
one warning. After the unpause it's `live` again, with one line in the log.

### Clean up

```bash
kind delete cluster --name opscope-live
```

## Topology

`topology.yaml` gives the graph something for every edge kind, and every kind of gap: an object
referred to that doesn't exist, and a selector that matches nothing. `topology-gateway-api.yaml`
adds a Gateway and an HTTPRoute, for a cluster with the Gateway API CRDs (no controller needed:
the graph reads the objects, not their status).

```bash
kind create cluster --name opscope-graph --kubeconfig /tmp/opscope-graph.kubeconfig
export KUBECONFIG=/tmp/opscope-graph.kubeconfig
until kubectl apply -f test/scenarios/topology.yaml; do sleep 2; done
```

Install the Gateway API CRDs with `kubectl apply --server-side -f` and the `standard-install.yaml`
of a [Gateway API release](https://github.com/kubernetes-sigs/gateway-api/releases), or copy them
from a cluster that has them, keeping each one's `api-approved.kubernetes.io` annotation; create
copies with `kubectl create` rather than `apply`, whose own annotation makes the HTTPRoute CRD too
large. Then:

```bash
kubectl apply -f test/scenarios/topology-gateway-api.yaml
```

Run Opscope with `OPSCOPE_KUBECONFIG=/tmp/opscope-graph.kubeconfig` and open the Topology page on
`graph-test` to see the map. The graph can also be checked without the UI, with a Go test that
reads the cluster, builds the graph the way Opscope does, and logs what it found:

```bash
cd backend
OPSCOPE_TEST_KUBECONFIG=/tmp/opscope-graph.kubeconfig OPSCOPE_TEST_EXPECT=all \
  go test ./internal/graph -run TestLiveCluster -v
```

In `graph-test`, it should find:

| Edge                                                    | From the scenario                                        |
| ------------------------------------------------------- | -------------------------------------------------------- |
| `owns`: Deployment `api` → its ReplicaSet → its pods    | ownerReferences; CronJob `nightly` → each Job → its pod  |
| `references`: pods and the `api` Deployment → `app-config`, `db-creds`, claim `data`, ServiceAccount `api` | volumes, env, the account; `flags` is missing but optional |
| `references`: StatefulSet `db` → claim `data-db-0`      | its claim template                                       |
| `references`: Ingress `web`, Gateway `edge` → `tls-cert` | TLS                                                     |
| `binds`: claims `data` and `data-db-0` → their volumes → `standard`; claim `pending` → missing `slow` | volumeName, storageClassName |
| `runs-on`: every pod → the node                         | nodeName                                                 |
| `selects`: Service `api` → its pods; `ghost` dangling   | `ghost`'s selector `app=nobody` matches nothing          |
| `endpoints`: the `api` EndpointSlice → its pods, ready  | the slice's targetRefs                                   |
| `routes`: Ingress `web` → `api` and missing `old-api`; HTTPRoute `store` → `api` and missing `missing-svc` | backends |
| `attaches`: HTTPRoute `store` → Gateway `edge` → GatewayClass `graph-test` | parentRefs, gatewayClassName |
| Pod `broken` → missing Secret `stripe-key` and missing ConfigMap `missing-config` | env and a volume |

`OPSCOPE_TEST_WATCH=2m` keeps the graph running for two minutes before it's compared with one built
from the cluster, while you change things (scale `api`, create `stripe-key`, relabel a pod, delete
`app-config`): the two must still match. To read as one of the users in `permissions.yaml`, use
their kubeconfig; for `team-a`, add `OPSCOPE_TEST_NAMESPACE=team-a OPSCOPE_TEST_BUILT=on_request`,
since it can't list across namespaces.

### Clean up

```bash
kind delete cluster --name opscope-graph
```
