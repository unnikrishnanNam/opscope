package resources

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func someObjects() *fake.Clientset {
	return fake.NewClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "web"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "web"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "db-password", Namespace: "data"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "api-1.1", Namespace: "web"}},
	)
}

func TestListNames(t *testing.T) {
	dynamic := fakeDynamic(
		object("Gateway", "web", "main", nil),
		object("GatewayClass", "", "nginx", nil),
	)
	index, err := ListNames(context.Background(), Clients{Kube: someObjects(), Dynamic: dynamic})
	if err != nil {
		t.Fatal(err)
	}

	want := []ObjectName{
		{Resource: "gatewayclasses", Name: "nginx"},
		{Resource: "gateways", Namespace: "web", Name: "main"},
		{Resource: "nodes", Name: "worker-1"},
		{Resource: "pods", Namespace: "web", Name: "api-1"},
		{Resource: "secrets", Namespace: "data", Name: "db-password"},
		{Resource: "services", Namespace: "web", Name: "api"},
	}
	// Namespaces are listed too; the fake has none, so they don't show here.
	if !reflect.DeepEqual(index.Objects, want) {
		t.Errorf("objects =\n  %+v\nwant\n  %+v", index.Objects, want)
	}
	if len(index.Skipped) != 0 {
		t.Errorf("skipped = %+v", index.Skipped)
	}
}

func TestListNamesLeavesOutEvents(t *testing.T) {
	index, err := ListNames(context.Background(), Clients{Kube: someObjects(), Dynamic: fakeDynamic()})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range index.Objects {
		if o.Resource == "events" {
			t.Errorf("events should be left out: %+v", o)
		}
	}
}

func TestListNamesWithoutGatewayAPI(t *testing.T) {
	index, err := ListNames(context.Background(), Clients{Kube: someObjects(), Dynamic: notInstalled()})
	if err != nil {
		t.Fatal(err)
	}
	var skipped []string
	for _, s := range index.Skipped {
		if s.Code != "not_installed" {
			t.Errorf("%s: code %q, want not_installed", s.Resource, s.Code)
		}
		skipped = append(skipped, s.Resource)
	}
	if want := []string{"gatewayclasses", "gateways", "httproutes"}; !slices.Equal(skipped, want) {
		t.Errorf("skipped = %v, want %v", skipped, want)
	}
	if len(index.Objects) != 4 {
		t.Errorf("the other kinds should still be there: %+v", index.Objects)
	}
}

func TestListNamesSkipsForbiddenKinds(t *testing.T) {
	kube := someObjects()
	kube.PrependReactor("list", "secrets", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("RBAC"))
	})
	index, err := ListNames(context.Background(), Clients{Kube: kube, Dynamic: fakeDynamic()})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Skipped{{Resource: "secrets", Code: "forbidden"}}; !reflect.DeepEqual(index.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", index.Skipped, want)
	}
	for _, o := range index.Objects {
		if o.Resource == "secrets" {
			t.Errorf("no secrets expected: %+v", o)
		}
	}
}

func TestListNamesReportsOtherFailures(t *testing.T) {
	kube := someObjects()
	kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("timeout")
	})
	index, err := ListNames(context.Background(), Clients{Kube: kube, Dynamic: fakeDynamic()})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Skipped{{Resource: "pods", Code: "failed", Error: "timeout"}}; !reflect.DeepEqual(index.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", index.Skipped, want)
	}
}

func TestListNamesFailsWhenNothingCanBeRead(t *testing.T) {
	kube := fake.NewClientset()
	kube.PrependReactor("list", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	dynamic := fakeDynamic()
	dynamic.PrependReactor("list", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	if _, err := ListNames(context.Background(), Clients{Kube: kube, Dynamic: dynamic}); err == nil {
		t.Fatal("want an error when the cluster can't be read at all")
	}
}

// Every kind in Listers must have rows with the meta method (from an
// embedded Meta), or its objects would silently be missing from the index.
func TestEveryListerRowHasMeta(t *testing.T) {
	clients := Clients{Kube: fake.NewClientset(), Dynamic: fakeDynamic()}
	hasMeta := reflect.TypeFor[interface{ meta() Meta }]()
	for name, lister := range Listers {
		rows, err := lister(context.Background(), clients, Query{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if row := reflect.TypeOf(rows).Elem(); !row.Implements(hasMeta) {
			t.Errorf("%s: %s doesn't embed Meta", name, row)
		}
	}
}
