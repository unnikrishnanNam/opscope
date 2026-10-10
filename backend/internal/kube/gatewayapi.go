package kube

// Gateway API (gateway.networking.k8s.io) is installed as custom resources,
// so client-go has no typed client for it. We use the dynamic client, which
// returns objects as plain maps ("unstructured"), and copy them into the
// small structs below. They mirror the Gateway API spec, but only the fields
// Opscope uses.

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ErrGatewayAPINotInstalled means the cluster doesn't serve Gateway API v1.
var ErrGatewayAPINotInstalled = errors.New("Gateway API (gateway.networking.k8s.io/v1) isn't installed on this cluster")

// GatewayAPI returns the address of a Gateway API v1 resource type, e.g. "gateways".
func GatewayAPI(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: resource}
}

// ListCustom lists a custom resource and converts each item into T.
// An empty namespace lists across all namespaces (or a cluster-wide kind).
// managedFields are dropped first: they're long, and nothing here uses them.
func ListCustom[T any](ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]T, error) {
	list, err := client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		// The API server answers 404 for a resource type it doesn't know.
		return nil, ErrGatewayAPINotInstalled
	}
	if err != nil {
		return nil, err
	}

	items := make([]T, 0, len(list.Items))
	for _, item := range list.Items {
		unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
		var obj T
		// Like json.Unmarshal, but from a map instead of bytes.
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &obj); err != nil {
			return nil, fmt.Errorf("reading %s %s: %w", gvr.Resource, item.GetName(), err)
		}
		items = append(items, obj)
	}
	return items, nil
}

// ObjectRef is how Gateway API points at another object (a parent Gateway,
// a backend Service, a certificate Secret). Optional fields are pointers so
// "not set" is nil.
type ObjectRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"` // parents only: a listener name
	Port        *int32  `json:"port"`
}

// RefName writes a reference briefly: just the name when it's the usual kind
// in the same namespace, otherwise with "Kind/" or "namespace/" in front.
func RefName(ref ObjectRef, ownNamespace, usualKind string) string {
	name := ref.Name
	if ref.Namespace != nil && *ref.Namespace != ownNamespace {
		name = *ref.Namespace + "/" + name
	}
	if ref.Kind != nil && *ref.Kind != usualKind {
		name = *ref.Kind + "/" + name
	}
	return name
}

// ---- Gateway ----

type Gateway struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		GatewayClassName string            `json:"gatewayClassName"`
		Listeners        []GatewayListener `json:"listeners"`
	} `json:"spec"`
	Status struct {
		Addresses []struct {
			Value string `json:"value"`
		} `json:"addresses"`
		Conditions []metav1.Condition `json:"conditions"`
		Listeners  []struct {
			Name           string             `json:"name"`
			AttachedRoutes int32              `json:"attachedRoutes"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"listeners"`
	} `json:"status"`
}

// GatewayListener is one port a Gateway accepts traffic on.
type GatewayListener struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"` // HTTP, HTTPS, TLS, TCP, UDP
	Hostname string `json:"hostname"` // only this host, if set
	TLS      *struct {
		CertificateRefs []ObjectRef `json:"certificateRefs"` // usually Secrets
	} `json:"tls"`
}

// Summary sums up a Gateway's conditions in one word, and why when something
// is wrong. "Accepted" means the controller took it on; "Programmed" means
// it's actually serving.
//   - NotAccepted, NotProgrammed: something is wrong; the message says what
//   - Programmed: serving
//   - Pending: no controller has reported on it yet
func (g *Gateway) Summary() (status, message string) {
	conditions := g.Status.Conditions
	if c := apimeta.FindStatusCondition(conditions, "Accepted"); c != nil && c.Status == metav1.ConditionFalse {
		return "NotAccepted", c.Message
	}
	if c := apimeta.FindStatusCondition(conditions, "Programmed"); c != nil {
		switch c.Status {
		case metav1.ConditionTrue:
			return "Programmed", ""
		case metav1.ConditionFalse:
			return "NotProgrammed", c.Message
		}
	}
	return "Pending", ""
}

// ---- HTTPRoute ----

type HTTPRoute struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		Hostnames  []string    `json:"hostnames"`
		ParentRefs []ObjectRef `json:"parentRefs"`
		Rules      []struct {
			BackendRefs []BackendRef `json:"backendRefs"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		// One entry per parent Gateway, written by that Gateway's controller.
		Parents []RouteParentStatus `json:"parents"`
	} `json:"status"`
}

type BackendRef struct {
	ObjectRef `json:",inline"`
	Weight    *int32 `json:"weight"`
}

type RouteParentStatus struct {
	ParentRef      ObjectRef          `json:"parentRef"`
	ControllerName string             `json:"controllerName"`
	Conditions     []metav1.Condition `json:"conditions"`
}

// Summary sums up a route across all its parent Gateways. The worst parent
// wins: one Gateway rejecting the route is worth knowing about.
//   - NotAccepted:    a Gateway refused the route
//   - UnresolvedRefs: accepted, but a backend can't be found
//   - Accepted:       every Gateway accepted it and every backend resolved
//   - Pending:        no Gateway has reported on it yet
func (r *HTTPRoute) Summary() (status, message string) {
	if len(r.Status.Parents) == 0 {
		return "Pending", ""
	}
	status = "Accepted"
	for _, p := range r.Status.Parents {
		accepted := apimeta.FindStatusCondition(p.Conditions, "Accepted")
		resolved := apimeta.FindStatusCondition(p.Conditions, "ResolvedRefs")
		switch {
		case accepted != nil && accepted.Status == metav1.ConditionFalse:
			return "NotAccepted", accepted.Message
		case resolved != nil && resolved.Status == metav1.ConditionFalse:
			status, message = "UnresolvedRefs", resolved.Message
		case accepted == nil && status == "Accepted":
			status = "Pending"
		}
	}
	return status, message
}

// ---- GatewayClass ----

// GatewayClass names the controller (like nginx or Envoy) that runs the
// Gateways using it.
type GatewayClass struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		ControllerName string `json:"controllerName"`
		Description    string `json:"description"`
	} `json:"spec"`
	Status struct {
		Conditions []metav1.Condition `json:"conditions"`
	} `json:"status"`
}

// Summary is Accepted, NotAccepted (with the controller's message) or
// Pending, when no controller has picked the class up.
func (c *GatewayClass) Summary() (status, message string) {
	if cond := apimeta.FindStatusCondition(c.Status.Conditions, "Accepted"); cond != nil {
		switch cond.Status {
		case metav1.ConditionTrue:
			return "Accepted", ""
		case metav1.ConditionFalse:
			return "NotAccepted", cond.Message
		}
	}
	return "Pending", ""
}

// ---- ReferenceGrant ----

// ReferenceGrant lives in a namespace and allows objects in other namespaces
// to refer to objects in it: without one, an HTTPRoute can't send traffic to
// a Service in another namespace.
type ReferenceGrant struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		From []struct {
			Group     string `json:"group"`
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
		} `json:"from"`
		To []struct {
			Group string  `json:"group"`
			Kind  string  `json:"kind"`
			Name  *string `json:"name"` // nil: every object of that kind
		} `json:"to"`
	} `json:"spec"`
}

// ListReferenceGrants lists every ReferenceGrant. They moved from v1beta1 to
// v1 later than the other kinds, so a cluster may serve only the older one.
func ListReferenceGrants(ctx context.Context, client dynamic.Interface) ([]ReferenceGrant, error) {
	grants, err := ListCustom[ReferenceGrant](ctx, client, GatewayAPI("referencegrants"), "")
	if errors.Is(err, ErrGatewayAPINotInstalled) {
		beta := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}
		return ListCustom[ReferenceGrant](ctx, client, beta, "")
	}
	return grants, err
}
