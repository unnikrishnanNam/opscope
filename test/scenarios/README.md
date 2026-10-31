# Test scenarios

Manifests that set up cases a healthy cluster doesn't have, to check Opscope's pages against. Run
them in a throwaway [kind](https://kind.sigs.k8s.io/) cluster with its own kubeconfig, never in a
cluster you care about.

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
