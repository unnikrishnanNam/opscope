package resources

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// detailClients returns fake clients holding the given objects, each stored
// under the resource named in the map key (e.g. "pods").
func detailClients(t *testing.T, objects map[string]runtime.Object) Clients {
	t.Helper()
	dyn := fakeDynamic()
	for resource, obj := range objects {
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{Object: raw}
		if err := dyn.Tracker().Create(kinds[resource].GVR, u, u.GetNamespace()); err != nil {
			t.Fatal(err)
		}
	}
	return Clients{Kube: fake.NewClientset(), Dynamic: dyn}
}

func TestSecretDetailHidesValues(t *testing.T) {
	clients := detailClients(t, map[string]runtime.Object{
		"secrets": &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "web", Name: "db",
				Annotations: map[string]string{
					// kubectl apply stores the whole Secret here, values included.
					"kubectl.kubernetes.io/last-applied-configuration": `{"data":{"password":"aHVudGVyMg=="}}`,
					"team": "payments",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"password": []byte("hunter2")},
		},
	})

	d, err := GetDetail(context.Background(), clients, "secrets", "web", "db")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "aHVudGVyMg"} {
		if strings.Contains(d.YAML, leak) {
			t.Errorf("secret value %q leaked into the YAML:\n%s", leak, d.YAML)
		}
	}
	if _, ok := d.Annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Error("last-applied-configuration annotation should be removed")
	}
	if d.Annotations["team"] != "payments" {
		t.Error("other annotations should stay")
	}
	if strings.Join(d.SecretKeys, ",") != "password" || !strings.Contains(d.YAML, "hidden") {
		t.Errorf("expected key names and a hidden placeholder, got keys %v and YAML:\n%s", d.SecretKeys, d.YAML)
	}
}

func TestPodDetail(t *testing.T) {
	yes := true
	clients := detailClients(t, map[string]runtime.Object{
		"pods": &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "web", Name: "api-1",
				OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "api", Controller: &yes}},
				ManagedFields:   []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
			},
			Spec: corev1.PodSpec{
				NodeName: "worker-1",
				Containers: []corev1.Container{{
					Name: "app", Image: "api:1.2",
					Ports: []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP, Name: "http"}},
				}},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}},
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "app", RestartCount: 4,
					State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}},
				}},
			},
		},
	})

	d, err := GetDetail(context.Background(), clients, "pods", "web", "api-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "Pod" || len(d.Owners) != 1 || d.Owners[0].Resource != "statefulsets" {
		t.Errorf("unexpected kind/owners: %s %+v", d.Kind, d.Owners)
	}
	c := d.Containers[0]
	if c.State != "Waiting" || c.StateReason != "CrashLoopBackOff" || c.LastExit != "OOMKilled (exit 137)" || c.Restarts != 4 {
		t.Errorf("unexpected container: %+v", c)
	}
	if len(c.Ports) != 1 || c.Ports[0] != "8080/TCP (http)" {
		t.Errorf("ports = %v", c.Ports)
	}
	if len(d.Conditions) != 1 || d.Conditions[0].Reason != "ContainersNotReady" {
		t.Errorf("conditions = %+v", d.Conditions)
	}
	if strings.Contains(d.YAML, "managedFields") {
		t.Error("managedFields should be removed from the YAML")
	}
	if !hasField(d, "Node", "worker-1") || !hasField(d, "Status", "CrashLoopBackOff") {
		t.Errorf("fields = %+v", d.Fields)
	}
}

func TestHTTPRouteDetail(t *testing.T) {
	route := object("HTTPRoute", "web", "api", map[string]any{
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "main"}},
			"rules": []any{
				map[string]any{
					"matches": []any{map[string]any{"path": map[string]any{"type": "PathPrefix", "value": "/api"}, "method": "GET"}},
					"backendRefs": []any{
						map[string]any{"name": "api-v1", "port": int64(80), "weight": int64(90)},
						map[string]any{"name": "api-v2", "port": int64(80), "weight": int64(10)},
					},
				},
				map[string]any{"backendRefs": []any{map[string]any{"name": "web", "port": int64(80)}}},
			},
		},
		"status": map[string]any{"parents": []any{map[string]any{
			"parentRef":  map[string]any{"name": "main"},
			"conditions": []any{condition("Accepted", "True", ""), condition("ResolvedRefs", "False", "api-v2 not found")},
		}}},
	})
	dyn := fakeDynamic(route)
	clients := Clients{Kube: fake.NewClientset(), Dynamic: dyn}

	d, err := GetDetail(context.Background(), clients, "httproutes", "web", "api")
	if err != nil {
		t.Fatal(err)
	}
	rules, parents := d.Tables[0], d.Tables[1]
	if got := rules.Rows[0]; got[1] != "GET /api (PathPrefix)" || got[3] != "api-v1:80 (weight 90), api-v2:80 (weight 10)" {
		t.Errorf("rule 1 = %q", got)
	}
	if got := rules.Rows[1]; got[1] != "everything" || got[3] != "web:80" {
		t.Errorf("rule 2 = %q", got)
	}
	if got := parents.Rows[0]; got[0] != "main" || got[1] != "True" || got[2] != "False" || got[3] != "api-v2 not found" {
		t.Errorf("parent status = %q", got)
	}
}

func TestDetailUnknownKind(t *testing.T) {
	_, err := GetDetail(context.Background(), Clients{Kube: fake.NewClientset(), Dynamic: fakeDynamic()}, "widgets", "", "x")
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("got %v, want ErrUnknownKind", err)
	}
}

func hasField(d *Detail, label, value string) bool {
	for _, f := range d.Fields {
		if f.Label == label && f.Value == value {
			return true
		}
	}
	return false
}
