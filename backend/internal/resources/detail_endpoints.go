package resources

// EndpointSlices: which pods a Service sends traffic to, and whether each is
// ready. Nobody browses EndpointSlices, so there's no list: a Service shows
// the endpoints of all its slices, and a slice has a small page of its own
// for links from events, the graph and signals to land on.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// serviceNameLabel names the Service a slice belongs to.
const serviceNameLabel = discoveryv1.LabelServiceName

func endpointSliceSections(d *Detail, obj *unstructured.Unstructured) error {
	var slice discoveryv1.EndpointSlice
	if err := convert(obj, &slice); err != nil {
		return err
	}
	if svc := slice.Labels[serviceNameLabel]; svc != "" {
		d.addLink("Service", svc, &Link{Resource: "services", Namespace: slice.Namespace, Name: svc})
	}
	d.add("Address type", string(slice.AddressType))
	d.add("Managed by", slice.Labels[discoveryv1.LabelManagedBy])

	t := endpointsTable(false)
	addEndpoints(&t, &slice)
	d.addTable(t)

	ports := Table{Title: "Ports", Columns: []string{"Name", "Port", "Protocol", "App protocol"}}
	for _, p := range slice.Ports {
		ports.addRow(text(deref(p.Name)), text(portText(p.Port)), text(protocolText(p)), text(deref(p.AppProtocol)))
	}
	d.addTable(ports)
	return nil
}

// serviceEndpoints adds the endpoints of every slice of a Service, read
// from the cache. A Service without a selector may have slices someone
// made by hand; those are shown too.
func serviceEndpoints(ctx context.Context, src Source, d *Detail) {
	items, err := ListOf[*discoveryv1.EndpointSlice](ctx, src, "endpointslices", Query{Namespace: d.Namespace})
	if err != nil {
		return
	}
	var mine []*discoveryv1.EndpointSlice
	for _, s := range items {
		if s.Labels[serviceNameLabel] == d.Name {
			mine = append(mine, s)
		}
	}
	slices.SortFunc(mine, func(a, b *discoveryv1.EndpointSlice) int { return strings.Compare(a.Name, b.Name) })

	t := endpointsTable(true)
	for _, s := range mine {
		addEndpoints(&t, s)
	}
	d.addTable(t)
}

func endpointsTable(withPorts bool) Table {
	columns := []string{"Address", "Pod", "State", "Node", "Zone"}
	if withPorts {
		columns = append(columns, "Ports")
	}
	return Table{Title: "Endpoints", Columns: columns}
}

// addEndpoints adds a row per endpoint of a slice: one row per endpoint,
// its addresses joined (dual-stack slices are one per address family).
func addEndpoints(t *Table, s *discoveryv1.EndpointSlice) {
	withPorts := len(t.Columns) == 6
	var ports []string
	for _, p := range s.Ports {
		port := portText(p.Port) + "/" + protocolText(p)
		if name := deref(p.Name); name != "" {
			port = name + " " + port
		}
		ports = append(ports, port)
	}
	for _, e := range s.Endpoints {
		pod := Cell{}
		if ref := e.TargetRef; ref != nil {
			pod.Text = ref.Name
			if ref.Kind == "Pod" {
				pod.Link = &Link{Resource: "pods", Namespace: orElse(ref.Namespace, s.Namespace), Name: ref.Name}
			} else {
				pod.Text = ref.Kind + "/" + ref.Name
			}
		}
		cells := []Cell{text(strings.Join(e.Addresses, ", ")), pod, text(endpointState(e.Conditions)), text(deref(e.NodeName)), text(deref(e.Zone))}
		if withPorts {
			cells = append(cells, text(strings.Join(ports, ", ")))
		}
		t.addRow(cells...)
	}
}

// endpointState puts an endpoint's conditions in words. Kubernetes says an
// unset ready (or serving) should be taken as ready; unset terminating as
// not terminating.
func endpointState(c discoveryv1.EndpointConditions) string {
	isTrue := func(b *bool, unset bool) bool {
		if b == nil {
			return unset
		}
		return *b
	}
	ready := isTrue(c.Ready, true)
	serving := isTrue(c.Serving, ready)
	switch {
	case isTrue(c.Terminating, false) && serving:
		return "Terminating, still serving"
	case isTrue(c.Terminating, false):
		return "Terminating"
	case ready:
		return "Ready"
	}
	return "Not ready"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func portText(p *int32) string {
	if p == nil {
		return "all" // a slice port without a number means every port
	}
	return fmt.Sprint(*p)
}

func protocolText(p discoveryv1.EndpointPort) string {
	if p.Protocol == nil {
		return "TCP"
	}
	return string(*p.Protocol)
}
