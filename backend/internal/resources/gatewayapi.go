package resources

// Gateway API (gateway.networking.k8s.io) is installed as custom resources,
// so client-go has no typed client for it. We use the dynamic client, which
// returns objects as plain maps ("unstructured"), and then copy the parts we
// need into small structs below. Those structs mirror the Gateway API spec,
// but only the fields OpScope shows.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ErrGatewayAPINotInstalled means the cluster doesn't serve Gateway API v1.
var ErrGatewayAPINotInstalled = errors.New("Gateway API (gateway.networking.k8s.io/v1) isn't installed on this cluster")

// gatewayAPI returns the address of a Gateway API resource type, e.g. "gateways".
func gatewayAPI(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: resource}
}

// listCustom lists a custom resource and converts each item into T.
// An empty namespace lists across all namespaces (or a cluster-wide kind).
func listCustom[T any](ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]T, error) {
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
		var obj T
		// Like json.Unmarshal, but from a map instead of bytes.
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &obj); err != nil {
			return nil, fmt.Errorf("reading %s %s: %w", gvr.Resource, item.GetName(), err)
		}
		items = append(items, obj)
	}
	return items, nil
}

// ---- Gateway ----

// gatewayObject is the part of a Gateway object we read.
type gatewayObject struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		GatewayClassName string            `json:"gatewayClassName"`
		Listeners        []GatewayListener `json:"listeners"`
	} `json:"spec"`
	Status struct {
		Addresses []struct {
			Value string `json:"value"`
		} `json:"addresses"`
		Conditions []metav1.Condition `json:"conditions"`
		Listeners  []struct {
			Name           string `json:"name"`
			AttachedRoutes int32  `json:"attachedRoutes"`
		} `json:"listeners"`
	} `json:"status"`
}

// GatewayListener is one port a Gateway accepts traffic on.
type GatewayListener struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`           // HTTP, HTTPS, TLS, TCP, UDP
	Hostname string `json:"hostname,omitempty"` // only this host, if set
}

// Gateway is one row of the gateway list.
type Gateway struct {
	Meta
	Status         string            `json:"status"`  // Programmed, NotProgrammed, NotAccepted or Pending
	Message        string            `json:"message"` // why, when something is wrong
	Class          string            `json:"class"`
	Listeners      []GatewayListener `json:"listeners"`
	Addresses      []string          `json:"addresses"`
	AttachedRoutes int32             `json:"attachedRoutes"` // routes attached across all listeners
}

func listGateways(ctx context.Context, client dynamic.Interface, q Query) ([]Gateway, error) {
	objects, err := listCustom[gatewayObject](ctx, client, gatewayAPI("gateways"), q.Namespace)
	if err != nil {
		return nil, err
	}

	rows := make([]Gateway, 0, len(objects))
	for _, g := range objects {
		row := Gateway{
			Meta:      metaOf(g.Metadata),
			Class:     g.Spec.GatewayClassName,
			Listeners: g.Spec.Listeners,
			Addresses: []string{},
		}
		if row.Listeners == nil {
			row.Listeners = []GatewayListener{}
		}
		row.Status, row.Message = gatewayStatus(g.Status.Conditions)
		for _, a := range g.Status.Addresses {
			row.Addresses = append(row.Addresses, a.Value)
		}
		for _, l := range g.Status.Listeners {
			row.AttachedRoutes += l.AttachedRoutes
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// gatewayStatus sums up a Gateway's conditions in one word. "Accepted" means
// the controller took it on; "Programmed" means it's actually serving.
func gatewayStatus(conditions []metav1.Condition) (string, string) {
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
	return "Pending", "" // no controller has reported on it yet
}

// ---- HTTPRoute ----

// objectRef is how Gateway API points at another object (a parent Gateway
// or a backend Service). Optional fields are pointers so "not set" is nil.
type objectRef struct {
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"` // parents only: a listener name
	Port        *int32  `json:"port"`
}

type httpRouteObject struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		Hostnames  []string    `json:"hostnames"`
		ParentRefs []objectRef `json:"parentRefs"`
		Rules      []struct {
			BackendRefs []objectRef `json:"backendRefs"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		// One entry per parent Gateway, written by that Gateway's controller.
		Parents []struct {
			Conditions []metav1.Condition `json:"conditions"`
		} `json:"parents"`
	} `json:"status"`
}

// HTTPRoute is one row of the HTTPRoute list.
type HTTPRoute struct {
	Meta
	Status    string   `json:"status"` // Accepted, NotAccepted, UnresolvedRefs or Pending
	Message   string   `json:"message"`
	Hostnames []string `json:"hostnames"`
	Parents   []string `json:"parents"`  // Gateways it attaches to, e.g. "main-gateway" or "infra/edge (https)"
	Backends  []string `json:"backends"` // where traffic goes, e.g. "web:80"
	Rules     int      `json:"rules"`
}

func listHTTPRoutes(ctx context.Context, client dynamic.Interface, q Query) ([]HTTPRoute, error) {
	objects, err := listCustom[httpRouteObject](ctx, client, gatewayAPI("httproutes"), q.Namespace)
	if err != nil {
		return nil, err
	}

	rows := make([]HTTPRoute, 0, len(objects))
	for _, r := range objects {
		ns := r.Metadata.Namespace
		row := HTTPRoute{
			Meta:      metaOf(r.Metadata),
			Hostnames: r.Spec.Hostnames,
			Parents:   []string{},
			Backends:  []string{},
			Rules:     len(r.Spec.Rules),
		}
		if row.Hostnames == nil {
			row.Hostnames = []string{}
		}

		for _, p := range r.Spec.ParentRefs {
			parent := refName(p, ns, "Gateway")
			if p.SectionName != nil {
				parent += " (" + *p.SectionName + ")"
			}
			row.Parents = append(row.Parents, parent)
		}

		seen := map[string]bool{} // the same backend often appears in several rules
		for _, rule := range r.Spec.Rules {
			for _, b := range rule.BackendRefs {
				backend := refName(b, ns, "Service")
				if b.Port != nil {
					backend += fmt.Sprintf(":%d", *b.Port)
				}
				if !seen[backend] {
					seen[backend] = true
					row.Backends = append(row.Backends, backend)
				}
			}
		}

		var parentConditions [][]metav1.Condition
		for _, p := range r.Status.Parents {
			parentConditions = append(parentConditions, p.Conditions)
		}
		row.Status, row.Message = routeStatus(parentConditions)

		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// refName writes a reference briefly: just the name when it's the usual kind
// in the same namespace, otherwise with "Kind/" or "namespace/" in front.
func refName(ref objectRef, ownNamespace, usualKind string) string {
	name := ref.Name
	if ref.Namespace != nil && *ref.Namespace != ownNamespace {
		name = *ref.Namespace + "/" + name
	}
	if ref.Kind != nil && *ref.Kind != usualKind {
		name = *ref.Kind + "/" + name
	}
	return name
}

// routeStatus sums up a route across all its parent Gateways. The worst
// parent wins: one Gateway rejecting the route is worth knowing about.
//   - NotAccepted:    a Gateway refused the route
//   - UnresolvedRefs: accepted, but a backend can't be found
//   - Accepted:       every Gateway accepted it and every backend resolved
//   - Pending:        no Gateway has reported on it yet
func routeStatus(parents [][]metav1.Condition) (string, string) {
	if len(parents) == 0 {
		return "Pending", ""
	}
	status, message := "Accepted", ""
	for _, conditions := range parents {
		accepted := apimeta.FindStatusCondition(conditions, "Accepted")
		resolved := apimeta.FindStatusCondition(conditions, "ResolvedRefs")
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

type gatewayClassObject struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		ControllerName string `json:"controllerName"`
		Description    string `json:"description"`
	} `json:"spec"`
	Status struct {
		Conditions []metav1.Condition `json:"conditions"`
	} `json:"status"`
}

// GatewayClass is one row of the GatewayClass list. A GatewayClass names the
// controller (like nginx or Envoy) that runs the Gateways using it.
type GatewayClass struct {
	Meta
	Status      string `json:"status"` // Accepted, NotAccepted or Pending
	Message     string `json:"message"`
	Controller  string `json:"controller"`
	Description string `json:"description"`
}

// listGatewayClasses ignores q.Namespace: GatewayClasses are cluster-wide.
func listGatewayClasses(ctx context.Context, client dynamic.Interface, q Query) ([]GatewayClass, error) {
	objects, err := listCustom[gatewayClassObject](ctx, client, gatewayAPI("gatewayclasses"), "")
	if err != nil {
		return nil, err
	}

	rows := make([]GatewayClass, 0, len(objects))
	for _, c := range objects {
		row := GatewayClass{
			Meta:        metaOf(c.Metadata),
			Controller:  c.Spec.ControllerName,
			Description: strings.TrimSpace(c.Spec.Description),
			Status:      "Pending",
		}
		if cond := apimeta.FindStatusCondition(c.Status.Conditions, "Accepted"); cond != nil {
			switch cond.Status {
			case metav1.ConditionTrue:
				row.Status = "Accepted"
			case metav1.ConditionFalse:
				row.Status, row.Message = "NotAccepted", cond.Message
			}
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}
