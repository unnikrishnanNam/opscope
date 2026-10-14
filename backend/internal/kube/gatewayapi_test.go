package kube

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func condition(kind string, status metav1.ConditionStatus, message string) metav1.Condition {
	return metav1.Condition{Type: kind, Status: status, Message: message, Reason: "Test"}
}

func TestGatewaySummary(t *testing.T) {
	var g Gateway
	g.Status.Conditions = []metav1.Condition{condition("Accepted", "True", ""), condition("Programmed", "False", "no address")}
	if status, msg := g.Summary(); status != "NotProgrammed" || msg != "no address" {
		t.Errorf("got %q %q", status, msg)
	}
	if status, _ := (&Gateway{}).Summary(); status != "Pending" {
		t.Errorf("no conditions: got %q, want Pending", status)
	}
}

func TestRouteSummary(t *testing.T) {
	tests := []struct {
		name    string
		parents [][]metav1.Condition
		want    string
	}{
		{"no parent has reported", nil, "Pending"},
		{"all accepted", [][]metav1.Condition{{condition("Accepted", "True", ""), condition("ResolvedRefs", "True", "")}}, "Accepted"},
		{"a backend is missing", [][]metav1.Condition{{condition("Accepted", "True", ""), condition("ResolvedRefs", "False", "")}}, "UnresolvedRefs"},
		{"one gateway rejects it", [][]metav1.Condition{
			{condition("Accepted", "True", "")},
			{condition("Accepted", "False", "hostname doesn't match")},
		}, "NotAccepted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r HTTPRoute
			for _, conditions := range tt.parents {
				r.Status.Parents = append(r.Status.Parents, RouteParentStatus{Conditions: conditions})
			}
			if got, _ := r.Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGatewayClassSummary(t *testing.T) {
	var c GatewayClass
	if status, _ := c.Summary(); status != "Pending" {
		t.Errorf("no conditions: got %q, want Pending", status)
	}
	c.Status.Conditions = []metav1.Condition{condition("Accepted", "False", "unsupported version")}
	if status, msg := c.Summary(); status != "NotAccepted" || msg != "unsupported version" {
		t.Errorf("got %q %q", status, msg)
	}
}

func TestSkip(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("no"))
	tests := []struct {
		err  error
		want string
	}{
		{ErrGatewayAPINotInstalled, "not_installed"},
		{forbidden, "forbidden"},
		{errors.New("connection refused"), "failed"},
	}
	for _, tt := range tests {
		if got := Skip("x", tt.err); got.Code != tt.want {
			t.Errorf("Skip(%v) = %q, want %q", tt.err, got.Code, tt.want)
		}
	}
}

// A cluster that serves ReferenceGrants only as v1beta1 still has them read.
func TestListReferenceGrantsFallsBackToBeta(t *testing.T) {
	beta := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		GatewayAPI("referencegrants"): "ReferenceGrantList",
		beta:                          "ReferenceGrantList",
	})
	grant := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1beta1",
		"kind":       "ReferenceGrant",
		"metadata": map[string]any{
			"name": "allow-shop", "namespace": "db",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
		},
		"spec": map[string]any{
			"from": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "shop"}},
			"to":   []any{map[string]any{"group": "", "kind": "Service"}},
		},
	}}
	if err := client.Tracker().Create(beta, grant, "db"); err != nil {
		t.Fatal(err)
	}
	// v1 isn't served: the API server answers 404 for it.
	client.PrependReactor("list", "referencegrants", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Version == "v1" {
			return true, nil, apierrors.NewNotFound(action.GetResource().GroupResource(), "")
		}
		return false, nil, nil
	})

	grants, err := ListReferenceGrants(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Name != "allow-shop" || grants[0].Spec.From[0].Namespace != "shop" {
		t.Fatalf("unexpected grants: %+v", grants)
	}
	if grants[0].ManagedFields != nil {
		t.Error("managedFields should be dropped")
	}
	if grants[0].Spec.To[0].Name != nil {
		t.Error("a grant without a name should allow every object of its kind")
	}
}
