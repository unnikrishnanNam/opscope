# Broken-app scenarios

Small apps that fail in one specific way each, for checking Opscope's diagnosis against a real
cluster. They all live in the `opscope-scenarios` namespace.

**Apply them only to a throwaway test cluster** (a local kind cluster, for example). They create
crash-looping and stuck pods on purpose, and `00-namespace.yaml` sets a ResourceQuota on the
namespace.

```bash
kubectl apply -f test/scenarios/
```

`rollout.yaml` needs a second step once its first version runs, to update it to an image that
doesn't exist:

```bash
kubectl -n opscope-scenarios rollout status deployment/rollout
```

```bash
kubectl -n opscope-scenarios set image deployment/rollout app=busybox:no-such-tag-opscope
```

Most reach their broken state within a minute; some (the OOM kill, the liveness probe) take a few
restarts, and pods count as stuck only after 5 minutes. Then open any object in the namespace in
Opscope.

To turn what the cluster shows into test data for the diagnosis (it only reads):

```bash
test/scenarios/capture.sh
```

To remove everything:

```bash
kubectl delete namespace opscope-scenarios
```

| File                       | What's wrong                                              | Root cause Opscope should find      |
| -------------------------- | --------------------------------------------------------- | ----------------------------------- |
| `missing-secret.yaml`      | an env var reads a Secret that doesn't exist              | the missing Secret                  |
| `missing-configmap.yaml`   | a volume mounts a ConfigMap that doesn't exist            | the missing ConfigMap               |
| `missing-key.yaml`         | an env var reads a key the ConfigMap doesn't have         | the ConfigMap, and the key          |
| `crash-loop.yaml`          | the container exits with code 1                           | the container's exit                |
| `oom.yaml`                 | the container uses more memory than its limit             | the memory limit                    |
| `liveness.yaml`            | the liveness probe always fails                           | the probe                           |
| `bad-image.yaml`           | the image tag doesn't exist                               | the image                           |
| `unreachable-registry.yaml`| the image's registry doesn't exist                        | the registry                        |
| `unschedulable.yaml`       | the pod asks for more CPU than any node has               | no node fits                        |
| `missing-account.yaml`     | the pods need a ServiceAccount that doesn't exist         | the missing ServiceAccount          |
| `over-quota.yaml`          | the pods need more memory than the namespace's quota allows | the ResourceQuota                 |
| `wrong-selector.yaml`      | a Service selects labels no pod has                       | the Service's selector              |
| `ingress.yaml`             | an Ingress sends traffic to a Service that doesn't exist, and one with no ready pods | the missing Service; the crash loop |
| `readiness.yaml`           | the readiness probe always fails                          | the probe                           |
| `init-waits.yaml`          | an init container waits for a Service that doesn't exist  | the init container                  |
| `evicted.yaml`             | the pod writes more to its disk than its limit            | the eviction                        |
| `rollout.yaml`             | a new revision's image doesn't exist (after the step above) | the image; the old revision serves |
| `job-failed.yaml`          | a Job's only attempt fails                                | the container's exit                |
| `cronjob-failed.yaml`      | a CronJob's runs fail (the first after a minute)          | the latest run's container          |
| `claim.yaml`               | a claim asks for a StorageClass that doesn't exist        | the missing StorageClass            |
| `autoscaler.yaml`          | an autoscaler scales by CPU on a cluster without metrics-server | the metrics it can't read     |
| `ingress-class.yaml`       | an Ingress for a class nobody runs, with a missing TLS Secret | the IngressClass; the Secret    |

Every pod sets a memory limit, since the namespace's quota requires one.

## Edge cases

Two more sets cover what the first one can't, on a cluster of their own. A kind cluster with a
worker node does, made with its own kubeconfig so `~/.kube/config` is left alone:

```bash
kind create cluster --name opscope-g7 --kubeconfig /tmp/opscope-g7.kubeconfig --config - <<'YAML'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes: [{role: control-plane}, {role: worker}]
YAML
```

**`gateway-api/`** needs the Gateway API CRDs (`gateways`, `httproutes`, `gatewayclasses`,
`referencegrants`) and no controller: its GatewayClass names one that doesn't exist.

| File          | What's wrong                                                         | Root cause Opscope should find        |
| ------------- | -------------------------------------------------------------------- | ------------------------------------- |
| `gateway.yaml` | the GatewayClass's controller isn't installed, so nothing runs the Gateway | the GatewayClass, after 5 minutes |
| `routes.yaml` | `shop` sends traffic to a Service that doesn't exist                 | the missing Service                   |
| `routes.yaml` | `catalog` sends traffic to a Service in another namespace without a ReferenceGrant | the missing grant      |
| `routes.yaml` | `reviews` does the same with a grant                                 | nothing: it's fine                    |

**`node-down/`** needs a worker node to stop. Label it, apply, wait for the pods to run, then stop
its container:

```bash
kubectl label node opscope-g7-worker opscope-scenario=node-down
```

```bash
kubectl apply -f test/scenarios/node-down/
```

```bash
docker stop opscope-g7-worker
```

Within a minute the node is NotReady and the pods, which still say Running, are diagnosed as
failing because of it. After five more minutes Kubernetes evicts them; their replacements can't be
scheduled, since only that node may run them.

To capture both into the test data:

```bash
NAMESPACES="opscope-edge opscope-backends opscope-node-down" OUT=backend/internal/diagnose/testdata/edge-cases test/scenarios/capture.sh
```

To remove everything, delete the cluster: `kind delete cluster --name opscope-g7`.
