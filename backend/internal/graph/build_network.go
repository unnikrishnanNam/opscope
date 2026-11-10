package graph

// Builders for Services, EndpointSlices, Ingresses and Gateway API.

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

const gatewayGroup = "gateway.networking.k8s.io"

func init() {
	Register(Builder{Name: "services.selector", Resource: "services", Refs: serviceSelector})
	Register(Builder{Name: "endpointslices.endpoints", Resource: "endpointslices", Refs: endpointSliceTargets})
	Register(Builder{Name: "ingresses.backends", Resource: "ingresses", Refs: ingressBackends})
	Register(Builder{Name: "ingresses.tls", Resource: "ingresses", Refs: ingressTLS})
	Register(Builder{Name: "httproutes.parents", Resource: "httproutes", Refs: routeParents})
	Register(Builder{Name: "httproutes.backends", Resource: "httproutes", Refs: routeBackends})
	Register(Builder{Name: "gateways.class", Resource: "gateways", Refs: gatewayClass})
	Register(Builder{Name: "gateways.certificates", Resource: "gateways", Refs: gatewayCertificates})
}

// serviceSelector: a Service selects the pods in its namespace whose labels
// match its selector. One with a selector that matches nothing is marked
// dangling. One without a selector has its endpoints set by hand (or is an
// ExternalName); it selects nothing and isn't dangling.
func serviceSelector(obj runtime.Object, r *Refs) {
	svc, ok := obj.(*corev1.Service)
	if !ok || len(svc.Spec.Selector) == 0 {
		return
	}
	selector := labels.SelectorFromSet(svc.Spec.Selector)
	r.Matching(Selects, Match{
		Resource:  "pods",
		Namespace: svc.Namespace,
		Selects: func(_ string, l map[string]string) bool {
			return selector.Matches(labels.Set(l))
		},
		Dangling: "matches no pods",
	}, "spec.selector", "")
}

// endpointSliceTargets: an EndpointSlice lists the pods behind its Service,
// and whether each is ready. The evidence says which.
func endpointSliceTargets(obj runtime.Object, r *Refs) {
	slice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return
	}
	for i, ep := range slice.Endpoints {
		t := ep.TargetRef
		if t == nil || t.Kind != "Pod" {
			continue
		}
		namespace := t.Namespace
		if namespace == "" {
			namespace = slice.Namespace
		}
		// A missing "ready" means unknown, which consumers are told to treat as ready.
		note := "ready"
		if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
			note = "not ready"
		}
		r.ToKind(Endpoints, "", "Pod", namespace, t.Name, string(t.UID), fmt.Sprintf("endpoints[%d].targetRef", i), note, false)
	}
}

func ingressBackends(obj runtime.Object, r *Refs) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return
	}
	if b := ing.Spec.DefaultBackend; b != nil && b.Service != nil {
		r.To(Routes, "services", ing.Namespace, b.Service.Name, "spec.defaultBackend.service.name", "default backend", false)
	}
	for i, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		host := rule.Host
		if host == "" {
			host = "*"
		}
		for j, p := range rule.HTTP.Paths {
			if p.Backend.Service == nil {
				continue // a backend that's a resource, not a Service
			}
			field := fmt.Sprintf("spec.rules[%d].http.paths[%d].backend.service.name", i, j)
			r.To(Routes, "services", ing.Namespace, p.Backend.Service.Name, field, host+p.Path, false)
		}
	}
}

func ingressTLS(obj runtime.Object, r *Refs) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return
	}
	for i, tls := range ing.Spec.TLS {
		r.To(References, "secrets", ing.Namespace, tls.SecretName, fmt.Sprintf("spec.tls[%d].secretName", i), "TLS", false)
	}
}

// ---- Gateway API ----

// Gateway API objects come as unstructured objects. These structs read the
// references from them; each reference's group, kind and namespace are
// optional, with defaults that depend on where the reference is.
type gatewayRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"`
	Port        *int32  `json:"port"`
}

func (ref gatewayRef) resolve(group, kind, namespace string) (string, string, string) {
	if ref.Group != nil {
		group = *ref.Group
	}
	if ref.Kind != nil {
		kind = *ref.Kind
	}
	if ref.Namespace != nil {
		namespace = *ref.Namespace
	}
	return group, kind, namespace
}

type httpRouteRefs struct {
	Spec struct {
		ParentRefs []gatewayRef `json:"parentRefs"`
		Rules      []struct {
			BackendRefs []gatewayRef `json:"backendRefs"`
		} `json:"rules"`
	} `json:"spec"`
}

type gatewayRefs struct {
	Spec struct {
		GatewayClassName string `json:"gatewayClassName"`
		Listeners        []struct {
			Name string `json:"name"`
			TLS  *struct {
				CertificateRefs []gatewayRef `json:"certificateRefs"`
			} `json:"tls"`
		} `json:"listeners"`
	} `json:"spec"`
}

// convert reads an unstructured object into one of the structs above.
func convert(obj runtime.Object, into any) (namespace string, ok bool) {
	u, isU := obj.(*unstructured.Unstructured)
	if !isU {
		return "", false
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, into); err != nil {
		return "", false
	}
	return u.GetNamespace(), true
}

// routeParents: an HTTPRoute attaches to the Gateways it names.
func routeParents(obj runtime.Object, r *Refs) {
	var route httpRouteRefs
	namespace, ok := convert(obj, &route)
	if !ok {
		return
	}
	for i, p := range route.Spec.ParentRefs {
		group, kind, ns := p.resolve(gatewayGroup, "Gateway", namespace)
		note := ""
		if p.SectionName != nil {
			note = "listener " + *p.SectionName
		}
		r.ToKind(Attaches, group, kind, ns, p.Name, "", fmt.Sprintf("spec.parentRefs[%d]", i), note, false)
	}
}

// routeBackends: an HTTPRoute sends traffic to the Services it names.
func routeBackends(obj runtime.Object, r *Refs) {
	var route httpRouteRefs
	namespace, ok := convert(obj, &route)
	if !ok {
		return
	}
	for i, rule := range route.Spec.Rules {
		for j, b := range rule.BackendRefs {
			group, kind, ns := b.resolve("", "Service", namespace)
			note := ""
			if b.Port != nil {
				note = fmt.Sprintf("port %d", *b.Port)
			}
			r.ToKind(Routes, group, kind, ns, b.Name, "", fmt.Sprintf("spec.rules[%d].backendRefs[%d]", i, j), note, false)
		}
	}
}

func gatewayClass(obj runtime.Object, r *Refs) {
	var gw gatewayRefs
	if _, ok := convert(obj, &gw); ok {
		r.To(Attaches, "gatewayclasses", "", gw.Spec.GatewayClassName, "spec.gatewayClassName", "", false)
	}
}

// gatewayCertificates: a Gateway's TLS listeners refer to their certificates.
func gatewayCertificates(obj runtime.Object, r *Refs) {
	var gw gatewayRefs
	namespace, ok := convert(obj, &gw)
	if !ok {
		return
	}
	for i, l := range gw.Spec.Listeners {
		if l.TLS == nil {
			continue
		}
		for j, c := range l.TLS.CertificateRefs {
			group, kind, ns := c.resolve("", "Secret", namespace)
			field := fmt.Sprintf("spec.listeners[%d].tls.certificateRefs[%d]", i, j)
			r.ToKind(References, group, kind, ns, c.Name, "", field, "listener "+l.Name, false)
		}
	}
}
