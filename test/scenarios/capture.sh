#!/bin/sh
# Captures the scenarios from the cluster kubectl points at into the test
# data of internal/diagnose: every object in the scenarios' namespaces, their
# events, and the cluster-wide objects the diagnosis reads (nodes, classes),
# with Gateway API kinds when the cluster has them. Only reads.
#
#   KUBECONFIG=... test/scenarios/capture.sh
#
# NAMESPACES (default opscope-scenarios) and OUT (default the scenarios'
# test data) capture another set, as for the edge cases:
#
#   NAMESPACES="opscope-edge opscope-backends opscope-node-down" \
#     OUT=backend/internal/diagnose/testdata/edge-cases test/scenarios/capture.sh
#
# Bookkeeping (managedFields, kubectl's last-applied copy, a node's image
# list) and Secret data are left out, so the files stay small and hold no
# secret values. Run it from the repository's root.
set -eu

out=${OUT:-backend/internal/diagnose/testdata/scenarios}
namespaces=${NAMESPACES:-opscope-scenarios}
gateway_api=gateways.gateway.networking.k8s.io,httproutes.gateway.networking.k8s.io,referencegrants.gateway.networking.k8s.io
taken=$(date -u +%Y-%m-%dT%H:%M:%SZ)
mkdir -p "$out"

{
  for ns in $namespaces; do
    kubectl get pods,deployments,replicasets,statefulsets,daemonsets,jobs,cronjobs,services,endpointslices,ingresses,configmaps,secrets,serviceaccounts,persistentvolumeclaims,resourcequotas,horizontalpodautoscalers,poddisruptionbudgets,events -n "$ns" -o json
    kubectl get "$gateway_api" -n "$ns" -o json 2>/dev/null || true # not every cluster has Gateway API
    kubectl get namespace "$ns" -o json
  done
  kubectl get nodes,storageclasses,ingressclasses -o json
  kubectl get gatewayclasses.gateway.networking.k8s.io -o json 2>/dev/null || true
} | python3 -c '
import json, sys
raw, decoder, i, items = sys.stdin.read(), json.JSONDecoder(), 0, []
while i < len(raw):
    while i < len(raw) and raw[i].isspace():
        i += 1
    if i == len(raw):
        break
    obj, i = decoder.raw_decode(raw, i)
    items += obj["items"] if obj.get("kind") == "List" else [obj]
for o in items:
    meta = o.get("metadata", {})
    meta.pop("managedFields", None)
    (meta.get("annotations") or {}).pop("kubectl.kubernetes.io/last-applied-configuration", None)
    if o["kind"] == "Node":
        o["status"].pop("images", None)
    if o["kind"] == "Secret":
        o.pop("data", None)
json.dump({"apiVersion": "v1", "kind": "List", "items": items}, sys.stdout, indent=1, sort_keys=True)
' > "$out/objects.json"

echo "$taken" > "$out/taken"
echo "Captured into $out at $taken. Check the root causes in scenario_test.go still hold."
