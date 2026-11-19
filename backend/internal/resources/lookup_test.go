package resources

import "testing"

func TestLookup(t *testing.T) {
	cases := []struct {
		resource, group, kind string
		namespaced            bool
	}{
		{"pods", "", "Pod", true},
		{"deployments", "apps", "Deployment", true},
		{"nodes", "", "Node", false},
		{"events", "", "Event", true},
		{"gateways", "gateway.networking.k8s.io", "Gateway", true},
		{"gatewayclasses", "gateway.networking.k8s.io", "GatewayClass", false},
		{"replicasets", "apps", "ReplicaSet", true},
		{"endpointslices", "discovery.k8s.io", "EndpointSlice", true},
		{"serviceaccounts", "", "ServiceAccount", true},
	}
	for _, c := range cases {
		k, ok := Lookup(c.resource)
		if !ok || k.KindName != c.kind || k.Namespaced != c.namespaced || k.GVR.Group != c.group {
			t.Errorf("Lookup(%q) = %+v, %v", c.resource, k, ok)
		}
		if k, ok := LookupKind(c.group, c.kind); !ok || k.Resource != c.resource {
			t.Errorf("LookupKind(%q, %q) = %+v, %v", c.group, c.kind, k, ok)
		}
	}
	// The same kind name in another group is another kind.
	if _, ok := LookupKind("", "Deployment"); ok {
		t.Error("found Deployment in the core group")
	}
	if _, ok := Lookup("rollouts"); ok {
		t.Error("found a kind Opscope doesn't know")
	}
	// Every kind is collected, with a way to list it directly.
	got := map[string]bool{}
	for _, k := range Kinds() {
		got[k.Resource] = true
		if k.KindName == "" {
			t.Errorf("%s has no kind name", k.Resource)
		}
		if _, ok := typedLists[k.Resource]; !ok && !k.Custom {
			t.Errorf("%s has no typed list for the Direct source", k.Resource)
		}
	}
	// Every kind with a detail view is collected, listed or not
	// (EndpointSlices have a detail view and no list).
	for resource := range kinds {
		if !got[resource] {
			t.Errorf("%s isn't collected", resource)
		}
	}
	if _, ok := Listers["endpointslices"]; ok {
		t.Error("EndpointSlices have no list: a Service shows its endpoints")
	}
}
