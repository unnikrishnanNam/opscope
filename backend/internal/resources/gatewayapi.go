package resources

// Gateway API rows. The objects are read with the dynamic client into the
// structs in internal/kube, and turned into table rows here.

import (
	"context"
	"fmt"
	"strings"

	"opscope/internal/kube"

	"k8s.io/client-go/dynamic"
)

// ErrGatewayAPINotInstalled means the cluster doesn't serve Gateway API v1.
var ErrGatewayAPINotInstalled = kube.ErrGatewayAPINotInstalled

// ---- Gateway ----

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
	objects, err := kube.ListCustom[kube.Gateway](ctx, client, kube.GatewayAPI("gateways"), q.Namespace)
	if err != nil {
		return nil, err
	}

	rows := make([]Gateway, 0, len(objects))
	for _, g := range objects {
		row := Gateway{
			Meta:      metaOf(g.ObjectMeta),
			Class:     g.Spec.GatewayClassName,
			Listeners: []GatewayListener{},
			Addresses: []string{},
		}
		for _, l := range g.Spec.Listeners {
			row.Listeners = append(row.Listeners, GatewayListener{Name: l.Name, Port: l.Port, Protocol: l.Protocol, Hostname: l.Hostname})
		}
		row.Status, row.Message = g.Summary()
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

// ---- HTTPRoute ----

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
	objects, err := kube.ListCustom[kube.HTTPRoute](ctx, client, kube.GatewayAPI("httproutes"), q.Namespace)
	if err != nil {
		return nil, err
	}

	rows := make([]HTTPRoute, 0, len(objects))
	for _, r := range objects {
		ns := r.Namespace
		row := HTTPRoute{
			Meta:      metaOf(r.ObjectMeta),
			Hostnames: r.Spec.Hostnames,
			Parents:   []string{},
			Backends:  []string{},
			Rules:     len(r.Spec.Rules),
		}
		if row.Hostnames == nil {
			row.Hostnames = []string{}
		}

		for _, p := range r.Spec.ParentRefs {
			parent := kube.RefName(p, ns, "Gateway")
			if p.SectionName != nil {
				parent += " (" + *p.SectionName + ")"
			}
			row.Parents = append(row.Parents, parent)
		}

		seen := map[string]bool{} // the same backend often appears in several rules
		for _, rule := range r.Spec.Rules {
			for _, b := range rule.BackendRefs {
				backend := kube.RefName(b.ObjectRef, ns, "Service")
				if b.Port != nil {
					backend += fmt.Sprintf(":%d", *b.Port)
				}
				if !seen[backend] {
					seen[backend] = true
					row.Backends = append(row.Backends, backend)
				}
			}
		}

		row.Status, row.Message = r.Summary()

		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// ---- GatewayClass ----

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
	objects, err := kube.ListCustom[kube.GatewayClass](ctx, client, kube.GatewayAPI("gatewayclasses"), "")
	if err != nil {
		return nil, err
	}

	rows := make([]GatewayClass, 0, len(objects))
	for _, c := range objects {
		row := GatewayClass{
			Meta:        metaOf(c.ObjectMeta),
			Controller:  c.Spec.ControllerName,
			Description: strings.TrimSpace(c.Spec.Description),
		}
		row.Status, row.Message = c.Summary()
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}
