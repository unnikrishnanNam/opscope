package diagnose

import (
	"fmt"
	"sort"
	"strings"

	"opscope/internal/graph"
	"opscope/internal/kube"

	corev1 "k8s.io/api/core/v1"
)

// ---- service.selector: a Service selects no pods ----

var serviceSelector = Rule{ID: "service.selector", Resources: []string{"services"}, Check: func(c *Context, n *graph.Node) []*Finding {
	svc, _ := c.Object(n).(*corev1.Service)
	if svc == nil || len(svc.Spec.Selector) == 0 || svc.Spec.Type == corev1.ServiceTypeExternalName {
		return nil
	}
	for _, e := range c.G.Out(n.ID) {
		if e.Type == graph.Selects {
			return nil
		}
	}

	var pods []*corev1.Pod
	for i := range c.S.Pods {
		if pod := &c.S.Pods[i]; pod.Namespace == svc.Namespace {
			pods = append(pods, pod)
		}
	}
	f := &Finding{
		Severity:   Failing,
		Confidence: Certain,
		Title:      "Its selector matches no pods",
		Detail: fmt.Sprintf("It sends traffic to pods labelled %s, and no pod in %s has those labels.",
			labelList(svc.Spec.Selector), svc.Namespace),
		Evidence: []Evidence{{Source: "spec", Text: "selector: " + labelList(svc.Spec.Selector)}},
		Next:     []Step{{Label: "kubectl get pods", Command: fmt.Sprintf("kubectl get pods -n %s --show-labels", svc.Namespace)}},
	}
	if hint := nearestLabels(svc.Spec.Selector, pods); hint != "" {
		f.Detail += " " + hint
	}
	return []*Finding{f}
}}

// nearestLabels looks for a near miss: pods with the same label key and a
// value one or two edits away from the selector's ("crash-loop" for
// "crashloop").
func nearestLabels(selector map[string]string, pods []*corev1.Pod) string {
	keys := make([]string, 0, len(selector))
	for k := range selector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		want := selector[k]
		counts := map[string]int{}
		for _, pod := range pods {
			if v, ok := pod.Labels[k]; ok && v != want {
				counts[v]++
			}
		}
		best, bestDistance := "", 3
		for v := range counts {
			if d := editDistance(v, want); d < bestDistance || (d == bestDistance && v < best) {
				best, bestDistance = v, d
			}
		}
		if best != "" {
			return fmt.Sprintf("Did you mean %s=%s? %s that label.", k, best, plural(counts[best], "pod has", "pods have"))
		}
	}
	return ""
}

// editDistance is the number of single-letter changes between two words.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(prev[j]+1, current[j-1]+1, prev[j-1]+cost)
		}
		prev = current
	}
	return prev[len(b)]
}

func labelList(labels map[string]string) string {
	var parts []string
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// ---- service.endpoints: a Service's pods aren't ready ----

var serviceEndpoints = Rule{ID: "service.endpoints", Resources: []string{"services"}, Check: func(c *Context, n *graph.Node) []*Finding {
	if n.State != graph.Failing && n.State != graph.Degraded {
		return nil
	}
	selected := 0
	var blames []string
	for _, e := range c.G.Out(n.ID) {
		if e.Type != graph.Selects {
			continue
		}
		selected++
		if pod := c.Node(e.To); pod.State != graph.OK {
			blames = append(blames, pod.ID)
		}
	}
	if selected == 0 {
		return nil // service.selector says why
	}
	f := &Finding{
		Severity:   graph2severity(n.State),
		Confidence: Certain,
		Blames:     blames,
		Next:       []Step{kubectl("describe", n)},
	}
	switch {
	case n.State == graph.Failing && selected == 1:
		f.Title = "The only pod it selects isn't ready"
		f.Detail = "It has nowhere to send traffic, so connections to it fail."
	case n.State == graph.Failing:
		f.Title = fmt.Sprintf("None of the %d pods it selects are ready", selected)
		f.Detail = "It has nowhere to send traffic, so connections to it fail."
	default:
		f.Title = fmt.Sprintf("%d of the %s it selects aren't ready", len(blames), plural(selected, "pod", "pods"))
		f.Detail = "It sends traffic only to the ready ones."
	}
	f.Evidence = []Evidence{{Source: "status", Text: "Endpoints: " + n.Status}}
	return []*Finding{f}
}}

// ---- route.backend: an Ingress or HTTPRoute sends traffic nowhere ----

var routeBackend = Rule{ID: "route.backend", Resources: []string{"ingresses", "httproutes"}, Check: func(c *Context, n *graph.Node) []*Finding {
	var findings []*Finding
	for _, e := range c.G.Out(n.ID) {
		target := c.Node(e.To)
		where := strings.Join(e.Via, ", ")
		if n.Resource == "httproutes" {
			where = strings.ReplaceAll(where, "rule", "Rule")
		}
		switch {
		case e.Type == graph.RoutesTo && target.State != graph.OK && target.State != graph.Progressing:
			f := &Finding{
				Severity:   graph2severity(target.State),
				Confidence: Certain,
				Title:      fmt.Sprintf("Requests for %s can't be served", where),
				Detail:     fmt.Sprintf("They go to %s, %s.", describe(target, n.Namespace), backendProblem(target)),
				Blames:     []string{target.ID},
			}
			if target.State == graph.Degraded {
				f.Title = fmt.Sprintf("Some requests for %s fail", where)
			}
			findings = append(findings, f)
		case e.Type == graph.Permits && target.State == graph.Missing:
			findings = append(findings, &Finding{
				Severity:   Failing,
				Confidence: Certain,
				Title:      fmt.Sprintf("%s isn't allowed to reach its backend in %s", where, target.Namespace),
				Detail:     "A route can send traffic to a Service in another namespace only when a ReferenceGrant there allows it.",
				Blames:     []string{target.ID},
			})
		}
	}
	// A route's own status often says the same in the controller's words.
	if route, ok := c.Object(n).(*kube.HTTPRoute); ok {
		if status, message := route.Summary(); status != "Accepted" && message != "" {
			for _, f := range findings {
				f.Evidence = append(f.Evidence, Evidence{Source: "condition", Text: status + ": " + message})
			}
		}
	}
	return findings
}}

func backendProblem(n *graph.Node) string {
	switch n.State {
	case graph.Missing:
		return "which doesn't exist"
	case graph.Unknown:
		return "which Opscope can't check"
	case graph.Degraded:
		return "and only some of its pods are ready"
	}
	return "which has no ready pods"
}

// ---- route.accepted: a Gateway doesn't take an HTTPRoute ----

// rejections explains why a Gateway refuses a route, by the reason it gives.
var rejections = map[string]string{
	"NotAllowedByListeners":      "none of its listeners allow routes from this namespace, or of this kind",
	"NoMatchingListenerHostname": "the route's hostnames match none of its listeners",
	"NoMatchingParent":           "the listener the route names (sectionName) doesn't exist",
	"UnsupportedValue":           "the route uses something its controller doesn't support",
}

var routeAccepted = Rule{ID: "route.accepted", Resources: []string{"httproutes"}, Check: func(c *Context, n *graph.Node) []*Finding {
	route, _ := c.Object(n).(*kube.HTTPRoute)
	if route == nil {
		return nil
	}
	var findings []*Finding
	for _, e := range c.G.Out(n.ID) {
		if gw := c.Node(e.To); e.Type == graph.Attaches && gw.State == graph.Missing {
			findings = append(findings, &Finding{Severity: Failing, Confidence: Certain,
				Title:  fmt.Sprintf("Its Gateway %s doesn't exist", qualified(gw)),
				Detail: "Nothing serves the route until it attaches to a Gateway.",
				Blames: []string{gw.ID}})
		}
	}
	for _, parent := range route.Status.Parents {
		for _, cond := range parent.Conditions {
			if cond.Type != "Accepted" || cond.Status != "False" {
				continue
			}
			gw := c.Node(key("gateways", refNamespace(parent.ParentRef, route.Namespace), parent.ParentRef.Name))
			f := &Finding{
				Severity:   Failing,
				Confidence: Certain,
				Title:      fmt.Sprintf("Gateway %s doesn't accept it", parent.ParentRef.Name),
				Detail:     cond.Message,
				Evidence:   []Evidence{conditionEvidence("Accepted", "False", cond.Reason, cond.Message, cond.LastTransitionTime)},
			}
			if why, ok := rejections[cond.Reason]; ok {
				f.Detail = "The Gateway says " + why + "."
			}
			if gw != nil {
				f.Blames = []string{gw.ID}
			}
			findings = append(findings, f)
		}
	}
	return findings
}}

func refNamespace(ref kube.ObjectRef, own string) string {
	if ref.Namespace != nil && *ref.Namespace != "" {
		return *ref.Namespace
	}
	return own
}

// ---- gateway.programmed: a Gateway isn't serving ----

var gatewayProgrammed = Rule{ID: "gateway.programmed", Resources: []string{"gateways"}, Check: func(c *Context, n *graph.Node) []*Finding {
	gw, _ := c.Object(n).(*kube.Gateway)
	if gw == nil || n.Status == "Programmed" {
		return nil
	}
	status, message := gw.Summary()
	var evidence []Evidence
	for _, cond := range gw.Status.Conditions {
		if cond.Status != "True" {
			evidence = append(evidence, conditionEvidence(cond.Type, string(cond.Status), cond.Reason, cond.Message, cond.LastTransitionTime))
		}
	}
	for _, e := range c.G.Out(n.ID) {
		class := c.Node(e.To)
		if e.Type != graph.Attaches || class.State == graph.OK {
			continue
		}
		f := &Finding{Severity: Failing, Confidence: Certain, Evidence: evidence, Blames: []string{class.ID},
			Title:  fmt.Sprintf("Its GatewayClass %s isn't accepted", class.Name),
			Detail: "A Gateway is run by the controller of its class; until a controller accepts the class, nothing runs it."}
		if class.State == graph.Missing {
			f.Title = fmt.Sprintf("Its GatewayClass %s doesn't exist", class.Name)
		}
		if class.State == graph.Progressing {
			f.Severity = Degraded
		}
		return []*Finding{f}
	}
	switch status {
	case "NotAccepted":
		return []*Finding{{Severity: Failing, Confidence: Certain, Evidence: evidence,
			Title: "Its controller doesn't accept it", Detail: message}}
	case "NotProgrammed":
		return []*Finding{{Severity: Failing, Confidence: Certain, Evidence: evidence,
			Title: "It isn't serving: its controller couldn't set it up", Detail: message}}
	}
	return nil // Pending: no controller has reported yet
}}

// ---- gatewayclass.accepted: no controller runs a GatewayClass ----

var gatewayClassAccepted = Rule{ID: "gatewayclass.accepted", Resources: []string{"gatewayclasses"}, Check: func(c *Context, n *graph.Node) []*Finding {
	class, _ := c.Object(n).(*kube.GatewayClass)
	if class == nil || n.State != graph.Failing {
		return nil
	}
	status, message := class.Summary()
	if status == "NotAccepted" {
		return []*Finding{{Severity: Failing, Confidence: Certain, Title: "Its controller doesn't accept it", Detail: message}}
	}
	return []*Finding{{
		Severity:   Failing,
		Confidence: Likely,
		Title:      "No controller has accepted it",
		Detail:     fmt.Sprintf("Its controller is %s. Is it installed and running?", class.Spec.ControllerName),
		Evidence:   []Evidence{{Source: "status", Text: "No Accepted condition, " + ago(c.Now.Sub(class.CreationTimestamp.Time)) + " after it was created"}},
	}}
}}

// ---- ingress.class: an Ingress's class or TLS Secret is missing ----

var ingressClass = Rule{ID: "ingress.class", Resources: []string{"ingresses"}, Check: func(c *Context, n *graph.Node) []*Finding {
	var findings []*Finding
	hasClass := false
	for _, e := range c.G.Out(n.ID) {
		target := c.Node(e.To)
		if target.Resource == "ingressclasses" {
			hasClass = true
		}
		if e.Type != graph.Uses || target.State != graph.Missing {
			continue
		}
		switch target.Resource {
		case "ingressclasses":
			findings = append(findings, &Finding{Severity: Failing, Confidence: Certain, Blames: []string{target.ID},
				Title:  fmt.Sprintf("Its IngressClass %s doesn't exist", target.Name),
				Detail: "No controller serves it, so its rules aren't in effect."})
		case "secrets":
			findings = append(findings, &Finding{Severity: Failing, Confidence: Certain, Blames: []string{target.ID},
				Title:  fmt.Sprintf("Its TLS Secret %s doesn't exist", target.Name),
				Detail: "Without its certificate, requests over HTTPS fail or get the controller's default certificate."})
		}
	}
	if !hasClass {
		findings = append(findings, &Finding{Severity: Info, Confidence: Likely,
			Title:  "It names no IngressClass, and the cluster has no default",
			Detail: "Some controllers serve Ingresses without a class; others ignore them."})
	}
	return findings
}}
