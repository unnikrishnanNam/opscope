# Broken-app scenarios

Small apps that fail in one specific way each, for checking Opscope's diagnosis against a real
cluster. They all live in the `opscope-scenarios` namespace.

**Apply them only to a throwaway test cluster** (a local kind cluster, for example). They create
crash-looping and stuck pods on purpose, and `00-namespace.yaml` sets a ResourceQuota on the
namespace.

```bash
kubectl apply -f test/scenarios/
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

Every pod sets a memory limit, since the namespace's quota requires one.
