package resources

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes/fake"
)

// resourceOf maps each Gateway API kind to its resource name.
var resourceOf = map[string]string{
	"Gateway":      "gateways",
	"HTTPRoute":    "httproutes",
	"GatewayClass": "gatewayclasses",
}

// fakeDynamic is a fake dynamic client that knows the Gateway API kinds.
// The fake needs to be told which "List" kind goes with each resource.
//
// Objects are added with Create and an explicit resource, because the fake
// otherwise guesses the resource from the kind with a simple plural rule,
// and that turns "Gateway" into "gatewaies".
func fakeDynamic(objects ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	listKinds := map[schema.GroupVersionResource]string{}
	for kind, resource := range resourceOf {
		listKinds[gatewayAPI(resource)] = kind + "List"
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	for _, obj := range objects {
		gvr := gatewayAPI(resourceOf[obj.GetKind()])
		if err := client.Tracker().Create(gvr, obj, obj.GetNamespace()); err != nil {
			panic(err)
		}
	}
	return client
}

// object builds an unstructured Gateway API object from a plain map,
// the same shape you'd write in YAML.
func object(kind, namespace, name string, fields map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": namespace},
	}
	for k, v := range fields {
		obj[k] = v
	}
	return &unstructured.Unstructured{Object: obj}
}

func condition(kind, status, message string) map[string]any {
	return map[string]any{
		"type": kind, "status": status, "message": message, "reason": "Test",
		"lastTransitionTime": "2026-01-01T00:00:00Z",
	}
}

func TestListGateways(t *testing.T) {
	client := fakeDynamic(object("Gateway", "web", "main", map[string]any{
		"spec": map[string]any{
			"gatewayClassName": "nginx",
			"listeners": []any{
				map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP"},
				map[string]any{"name": "https", "port": int64(443), "protocol": "HTTPS", "hostname": "app.local"},
			},
		},
		"status": map[string]any{
			"addresses":  []any{map[string]any{"type": "IPAddress", "value": "10.99.38.200"}},
			"conditions": []any{condition("Accepted", "True", ""), condition("Programmed", "True", "")},
			"listeners": []any{
				map[string]any{"name": "http", "attachedRoutes": int64(2)},
				map[string]any{"name": "https", "attachedRoutes": int64(1)},
			},
		},
	}))

	gateways, err := listGateways(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	g := gateways[0]
	if g.Status != "Programmed" || g.Class != "nginx" || g.AttachedRoutes != 3 {
		t.Errorf("unexpected gateway row: %+v", g)
	}
	if len(g.Listeners) != 2 || g.Listeners[1].Hostname != "app.local" || g.Listeners[1].Port != 443 {
		t.Errorf("unexpected listeners: %+v", g.Listeners)
	}
	if len(g.Addresses) != 1 || g.Addresses[0] != "10.99.38.200" {
		t.Errorf("addresses = %v", g.Addresses)
	}
}

func TestGatewayStatus(t *testing.T) {
	notProgrammed, msg := gatewayStatus(conditionsFrom(t,
		condition("Accepted", "True", ""), condition("Programmed", "False", "no address")))
	if notProgrammed != "NotProgrammed" || msg != "no address" {
		t.Errorf("got %q %q", notProgrammed, msg)
	}
	if status, _ := gatewayStatus(nil); status != "Pending" {
		t.Errorf("no conditions: got %q, want Pending", status)
	}
}

func TestListHTTPRoutes(t *testing.T) {
	client := fakeDynamic(object("HTTPRoute", "web", "api", map[string]any{
		"spec": map[string]any{
			"hostnames": []any{"api.local"},
			"parentRefs": []any{
				map[string]any{"name": "main"},
				map[string]any{"name": "edge", "namespace": "infra", "sectionName": "https"},
			},
			"rules": []any{
				map[string]any{"backendRefs": []any{map[string]any{"name": "api", "port": int64(8080)}}},
				map[string]any{"backendRefs": []any{
					map[string]any{"name": "api", "port": int64(8080)}, // same backend again
					map[string]any{"name": "legacy", "namespace": "old", "port": int64(80)},
				}},
			},
		},
		"status": map[string]any{
			"parents": []any{
				map[string]any{"conditions": []any{condition("Accepted", "True", ""), condition("ResolvedRefs", "True", "")}},
				map[string]any{"conditions": []any{condition("Accepted", "True", ""), condition("ResolvedRefs", "False", "service old/legacy not found")}},
			},
		},
	}))

	routes, err := listHTTPRoutes(context.Background(), client, Query{Namespace: "web"})
	if err != nil {
		t.Fatal(err)
	}
	r := routes[0]
	if got := strings.Join(r.Parents, ", "); got != "main, infra/edge (https)" {
		t.Errorf("parents = %q", got)
	}
	if got := strings.Join(r.Backends, ", "); got != "api:8080, old/legacy:80" {
		t.Errorf("backends = %q", got)
	}
	if r.Status != "UnresolvedRefs" || r.Message != "service old/legacy not found" || r.Rules != 2 {
		t.Errorf("unexpected route row: %+v", r)
	}
}

func TestRouteStatus(t *testing.T) {
	tests := []struct {
		name    string
		parents [][]any
		want    string
	}{
		{"no parent has reported", nil, "Pending"},
		{"all accepted", [][]any{{condition("Accepted", "True", ""), condition("ResolvedRefs", "True", "")}}, "Accepted"},
		{"one gateway rejects it", [][]any{
			{condition("Accepted", "True", "")},
			{condition("Accepted", "False", "hostname doesn't match")},
		}, "NotAccepted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var parents [][]metav1.Condition
			for _, p := range tt.parents {
				parents = append(parents, conditionsFrom(t, p...))
			}
			if got, _ := routeStatus(parents); got != tt.want {
				t.Errorf("routeStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListGatewayClasses(t *testing.T) {
	class := object("GatewayClass", "", "nginx", map[string]any{
		"spec":   map[string]any{"controllerName": "gateway.nginx.org/nginx-gateway-controller"},
		"status": map[string]any{"conditions": []any{condition("Accepted", "True", "")}},
	})
	classes, err := listGatewayClasses(context.Background(), fakeDynamic(class), Query{Namespace: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if c := classes[0]; c.Status != "Accepted" || c.Controller != "gateway.nginx.org/nginx-gateway-controller" {
		t.Errorf("unexpected class row: %+v", c)
	}
}

// notInstalled makes every list call fail the way an API server does for an
// unknown resource type: with a 404.
func notInstalled() *dynamicfake.FakeDynamicClient {
	client := fakeDynamic()
	client.PrependReactor("list", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(action.GetResource().GroupResource(), "")
	})
	return client
}

func TestGatewayAPINotInstalled(t *testing.T) {
	_, err := listGateways(context.Background(), notInstalled(), Query{})
	if !errors.Is(err, ErrGatewayAPINotInstalled) {
		t.Fatalf("got %v, want ErrGatewayAPINotInstalled", err)
	}

	// The overview still works, just without Gateway API counts.
	o, err := GetOverview(context.Background(), Clients{Kube: fake.NewClientset(), Dynamic: notInstalled()}, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if o.GatewayAPI != nil {
		t.Errorf("expected no Gateway API counts, got %+v", o.GatewayAPI)
	}
}

// conditionsFrom converts condition maps into typed conditions.
func conditionsFrom(t *testing.T, maps ...any) []metav1.Condition {
	t.Helper()
	var holder struct {
		Conditions []metav1.Condition `json:"conditions"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(map[string]any{"conditions": maps}, &holder); err != nil {
		t.Fatal(err)
	}
	return holder.Conditions
}
