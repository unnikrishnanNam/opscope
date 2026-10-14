package snapshot

import (
	"slices"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestFromYAML(t *testing.T) {
	s, err := FromYAML([]byte(`
# a List, as kubectl get -o yaml writes it
apiVersion: v1
kind: List
items:
  - apiVersion: v1
    kind: Pod
    metadata: {name: web, namespace: shop, uid: pod-1, managedFields: [{manager: kubectl}]}
  - apiVersion: v1
    kind: Secret
    metadata: {name: stripe, namespace: shop}
    data: {api-key: c2VjcmV0}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: settings, namespace: shop}
data: {b: "2", a: "1"}
---
apiVersion: v1
kind: Event
metadata: {name: e1, namespace: shop}
type: Warning
reason: BackOff
involvedObject: {kind: Pod, name: web, uid: pod-1}
---
apiVersion: v1
kind: Event
metadata: {name: e2, namespace: shop}
type: Normal
reason: Pulled
involvedObject: {kind: Pod, name: web, uid: pod-1}
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: edge, namespace: infra}
spec: {gatewayClassName: nginx}
---
apiVersion: example.com/v1
kind: Widget
metadata: {name: ignored}
`))
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []Key{{"pods", "shop", "web"}, {"secrets", "shop", "stripe"}, {"configmaps", "shop", "settings"}, {"gateways", "infra", "edge"}} {
		if _, ok := s.Get(key.Resource, key.Namespace, key.Name); !ok {
			t.Errorf("%s not found", key)
		}
	}
	if s.Size() != 4 {
		t.Errorf("Size() = %d, want 4 (the Widget is ignored)", s.Size())
	}
	if pod, _ := s.Get("pods", "shop", "web"); pod.(*corev1.Pod).ManagedFields != nil {
		t.Error("managedFields should be dropped")
	}
	if cm, _ := s.Get("configmaps", "shop", "settings"); !slices.Equal(cm.(*ConfigMap).Keys, []string{"a", "b"}) {
		t.Errorf("keys = %v", cm.(*ConfigMap).Keys)
	}
	if events := s.EventsFor("pod-1"); len(events) != 1 || events[0].Reason != "BackOff" {
		t.Errorf("only the Warning event is kept, got %d", len(events))
	}
}

func TestFromYAMLReportsBadInput(t *testing.T) {
	if _, err := FromYAML([]byte("kind: Pod\nmetadata: [not, a, map]")); err == nil {
		t.Error("want an error for an object that doesn't fit its kind")
	}
}

// A derived value can be built from another, as the diagnosis is from the
// graph, and each is built once even when asked for at the same time.
func TestDerived(t *testing.T) {
	s := &Snapshot{}
	builds := 0
	graph := func() any { builds++; return "graph" }
	diagnosis := func() any { return s.Derived("graph", graph).(string) + "+rules" }

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if got := s.Derived("diagnosis", diagnosis); got != "graph+rules" {
				t.Errorf("got %v", got)
			}
		})
	}
	wg.Wait()
	if builds != 1 {
		t.Errorf("the graph was built %d times", builds)
	}
}
