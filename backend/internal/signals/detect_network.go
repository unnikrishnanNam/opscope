package signals

import (
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/graph"
)

// Detectors on Services, routes and gateways.

func init() {
	Default.Register(Detector{
		Name:   "services.endpoints",
		Kinds:  []string{"services"},
		Types:  []string{"selector_matches_nothing", "no_ready_endpoints"},
		Detect: serviceEndpoints,
		Triggers: []Trigger{
			// A pod bears on the Services that select it. Triggers see the
			// pod before and after a change, so a Service it stops matching
			// is checked too.
			{Resource: "pods", Targets: func(obj runtime.Object, c Context) []Key {
				pod, ok := obj.(*corev1.Pod)
				if !ok {
					return nil
				}
				var out []Key
				for _, o := range c.List("services", pod.Namespace) {
					svc := o.(*corev1.Service)
					if len(svc.Spec.Selector) > 0 && labels.SelectorFromSet(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) {
						out = append(out, Key{"services", svc.Namespace, svc.Name})
					}
				}
				return out
			}},
			{Resource: "endpointslices", Targets: func(obj runtime.Object, c Context) []Key {
				s, ok := obj.(*discoveryv1.EndpointSlice)
				if !ok || s.Labels[discoveryv1.LabelServiceName] == "" {
					return nil
				}
				return []Key{{"services", s.Namespace, s.Labels[discoveryv1.LabelServiceName]}}
			}},
		},
	})
	Default.Register(Detector{
		Name:   "routes.backends",
		Kinds:  []string{"ingresses", "httproutes"},
		Types:  []string{"backend_missing"},
		Detect: routeBackends,
		Triggers: []Trigger{
			// A Service that comes or goes may be what a route sends to. An
			// HTTPRoute may name a Service in another namespace.
			{Resource: "services", Targets: func(obj runtime.Object, c Context) []Key {
				return append(keysOf("ingresses", c.List("ingresses", namespaceOf(obj))), keysOf("httproutes", c.List("httproutes", ""))...)
			}},
		},
	})
	Default.Register(Detector{
		Name:   "gatewayapi.conditions",
		Kinds:  []string{"httproutes", "gateways", "gatewayclasses"},
		Types:  []string{"route_not_accepted", "gateway_not_programmed", "gatewayclass_not_accepted"},
		Detect: gatewayConditions,
	})
}

// serviceEndpoints: a Service whose selector matches no pods, and one that
// selects pods none of which is ready. A Service without a selector has
// its endpoints set by hand, and is left alone.
func serviceEndpoints(obj runtime.Object, c Context) []Found {
	svc, ok := obj.(*corev1.Service)
	if !ok || svc.DeletionTimestamp != nil || len(svc.Spec.Selector) == 0 || !c.Readable("pods") {
		return nil
	}
	selector := labels.SelectorFromSet(svc.Spec.Selector)
	var selected []*corev1.Pod
	for _, p := range c.List("pods", svc.Namespace) {
		if pod := p.(*corev1.Pod); pod.DeletionTimestamp == nil && selector.Matches(labels.Set(pod.Labels)) {
			selected = append(selected, pod)
		}
	}
	selectorEvidence := Evidence{Source: FromField, Field: "spec.selector", Value: labels.Set(svc.Spec.Selector).String(), ResourceVersion: svc.ResourceVersion}
	if len(selected) == 0 {
		return []Found{{
			Type:     "selector_matches_nothing",
			Message:  fmt.Sprintf("Its selector %s matches no pods", labels.Set(svc.Spec.Selector)),
			Evidence: []Evidence{selectorEvidence},
		}}
	}
	if !c.Readable("endpointslices") {
		return nil
	}
	var slices_ []*discoveryv1.EndpointSlice
	for _, o := range c.List("endpointslices", svc.Namespace) {
		if s := o.(*discoveryv1.EndpointSlice); s.Labels[discoveryv1.LabelServiceName] == svc.Name {
			slices_ = append(slices_, s)
		}
	}
	if slices_ == nil {
		return nil // the endpoints controller hasn't made any yet; nothing to say
	}
	ready := 0
	var evidence []Evidence
	for _, s := range slices_ {
		for i, e := range s.Endpoints {
			if e.Conditions.Ready == nil || *e.Conditions.Ready {
				ready++
			}
			evidence = append(evidence, Evidence{Source: FromField, Field: fmt.Sprintf("endpointslices/%s: endpoints[%d].conditions.ready", s.Name, i),
				Value: fmt.Sprint(e.Conditions.Ready != nil && *e.Conditions.Ready), ResourceVersion: s.ResourceVersion})
		}
	}
	if ready > 0 {
		return nil
	}
	return []Found{{
		Type:     "no_ready_endpoints",
		Message:  fmt.Sprintf("It selects %d %s, and none is ready", len(selected), plural(int64(len(selected)), "pod", "pods")),
		Since:    lastUnready(selected),
		Evidence: append([]Evidence{selectorEvidence}, evidence...),
	}}
}

// lastUnready is when the last of these pods stopped being ready, from
// their Ready conditions: when the Service lost its last ready endpoint.
// It's zero when a pod doesn't say, and the type's wait then counts from
// when Opscope saw it.
func lastUnready(pods []*corev1.Pod) time.Time {
	var last time.Time
	for _, pod := range pods {
		var since time.Time
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
				since = cond.LastTransitionTime.Time
			}
		}
		if since.IsZero() {
			return time.Time{}
		}
		if since.After(last) {
			last = since
		}
	}
	return last
}

// routeBackends: an Ingress or HTTPRoute that sends traffic to a Service
// that doesn't exist. The references are the graph's own (graph.RefsOf),
// so the Topology page shows the same missing Service.
func routeBackends(obj runtime.Object, c Context) []Found {
	resource := ""
	switch obj.(type) {
	case *networkingv1.Ingress:
		resource = "ingresses"
	case *unstructured.Unstructured:
		resource = "httproutes"
	default:
		return nil
	}
	if m, ok := obj.(metav1.Object); !ok || m.GetDeletionTimestamp() != nil || !c.Readable("services") {
		return nil
	}
	var missing []string
	var evidence []Evidence
	rv := obj.(metav1.Object).GetResourceVersion()
	for _, ref := range graph.RefsOf(resource, obj) {
		t := ref.Target
		if ref.Kind != graph.Routes || t == nil || t.Resource != "services" || c.Get("services", t.Namespace, t.Name) != nil {
			continue
		}
		name := t.Name
		if t.Namespace != namespaceOf(obj) {
			name = t.Namespace + "/" + t.Name
		}
		if !slices.Contains(missing, name) {
			missing = append(missing, name)
		}
		evidence = append(evidence, Evidence{Source: FromField, Field: ref.Field, Value: name, ResourceVersion: rv})
	}
	if missing == nil {
		return nil
	}
	return []Found{{
		Type:     "backend_missing",
		Message:  fmt.Sprintf("It sends traffic to %s %s, which %s exist", plural(int64(len(missing)), "Service", "Services"), strings.Join(missing, ", "), plural(int64(len(missing)), "doesn't", "don't")),
		Evidence: evidence,
	}}
}

// gatewayConditions reads Gateway API status: a route its parent doesn't
// accept or whose references don't resolve, a Gateway its controller
// hasn't programmed, a GatewayClass no controller accepts.
func gatewayConditions(obj runtime.Object, c Context) []Found {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok || u.GetDeletionTimestamp() != nil {
		return nil
	}
	var status struct {
		Status struct {
			Conditions []metav1.Condition `json:"conditions"`
			Parents    []struct {
				ParentRef struct {
					Name string `json:"name"`
				} `json:"parentRef"`
				Conditions []metav1.Condition `json:"conditions"`
			} `json:"parents"`
		} `json:"status"`
	}
	if runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &status) != nil {
		return nil
	}
	rv := u.GetResourceVersion()
	falseCondition := func(conds []metav1.Condition, typ string) (*metav1.Condition, int) {
		for i := range conds {
			if conds[i].Type == typ && conds[i].Status == metav1.ConditionFalse {
				return &conds[i], i
			}
		}
		return nil, -1
	}
	switch u.GetKind() {
	case "HTTPRoute":
		var f Found
		for p, parent := range status.Status.Parents {
			for _, typ := range []string{"Accepted", "ResolvedRefs"} {
				cond, i := falseCondition(parent.Conditions, typ)
				if cond == nil {
					continue
				}
				f.Evidence = append(f.Evidence, Evidence{Source: FromField, Field: fmt.Sprintf("status.parents[%d].conditions[%d]", p, i),
					Value: fmt.Sprintf("%s=False (%s)", typ, cond.Reason), ResourceVersion: rv})
				if f.Message == "" {
					f.Message = fmt.Sprintf("Gateway %s: %s", parent.ParentRef.Name, orElse(cond.Message, typ+" is false: "+cond.Reason))
					f.Since = cond.LastTransitionTime.Time
				}
			}
		}
		if f.Evidence != nil {
			f.Type = "route_not_accepted"
			return []Found{f}
		}
	case "Gateway":
		if cond, i := falseCondition(status.Status.Conditions, "Programmed"); cond != nil {
			return []Found{conditionFound("gateway_not_programmed", "Not programmed", cond, i, rv)}
		}
	case "GatewayClass":
		if cond, i := falseCondition(status.Status.Conditions, "Accepted"); cond != nil {
			return []Found{conditionFound("gatewayclass_not_accepted", "Not accepted by its controller", cond, i, rv)}
		}
	}
	return nil
}

func conditionFound(typ, what string, cond *metav1.Condition, i int, rv string) Found {
	return Found{
		Type: typ, Since: cond.LastTransitionTime.Time,
		Message: what + ": " + orElse(cond.Message, cond.Reason),
		Evidence: []Evidence{{Source: FromField, Field: fmt.Sprintf("status.conditions[%d]", i),
			Value: fmt.Sprintf("%s=False (%s)", cond.Type, cond.Reason), ResourceVersion: rv}},
	}
}

func namespaceOf(obj runtime.Object) string {
	if m, ok := obj.(metav1.Object); ok {
		return m.GetNamespace()
	}
	return ""
}

func keysOf(resource string, objects []runtime.Object) []Key {
	var out []Key
	for _, o := range objects {
		if m, ok := o.(metav1.Object); ok {
			out = append(out, Key{resource, m.GetNamespace(), m.GetName()})
		}
	}
	return out
}
