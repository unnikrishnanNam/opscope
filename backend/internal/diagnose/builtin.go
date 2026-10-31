package diagnose

import (
	"fmt"
	"sort"
	"strings"

	"opscope/internal/graph"
)

// rules lists every rule. Adding one means a line here.
var rules = []Rule{
	podConfig,
	podMount,
	podImage,
	podCrash,
	podUnschedulable,
	podNodeDown,
	nodeNotReady,
	workloadUnavailable,
	createFailed,
	podNotReady,
	podEvicted,
	podInit,
	nodePressure,
	deploymentRollout,
	jobFailed,
	hpaScaling,
	pvcPending,
	serviceSelector,
	serviceEndpoints,
	routeBackend,
	routeAccepted,
	gatewayProgrammed,
	gatewayClassAccepted,
	ingressClass,
}

// rulesFor finds the rules for a resource type.
var rulesFor = func() map[string][]Rule {
	m := map[string][]Rule{}
	for _, rule := range rules {
		for _, resource := range rule.Resources {
			m[resource] = append(m[resource], rule)
		}
	}
	return m
}()

// missing is the finding every missing object gets: it's referred to, but
// doesn't exist. It's a root cause: nothing else can explain an absence.
func missing(c *Context, n *graph.Node) *Finding {
	// Pods are left out when their workload is listed: "Deployment web"
	// says it better than its thirty pods.
	referrers := map[string]bool{}
	for _, e := range c.G.In(n.ID) {
		referrers[e.From] = true
	}
	var refs []string
	for _, e := range c.G.In(n.ID) {
		from := c.Node(e.From)
		if top := topOwner(c.G, e.From); top != e.From && referrers[top] {
			continue
		}
		ref := describe(from, n.Namespace)
		if len(e.Via) > 0 {
			ref += " (" + strings.Join(e.Via, ", ") + ")"
		}
		refs = append(refs, ref)
	}

	f := &Finding{
		Rule:       "missing",
		Severity:   Failing,
		Confidence: Certain,
		Title:      fmt.Sprintf("%s %s doesn't exist", n.Kind, qualified(n)),
		Detail:     "It's needed by " + list(refs) + ".",
		Evidence:   []Evidence{{Source: "reference", Text: "Opscope read every " + n.Kind + " in the cluster and found no " + qualified(n) + "."}},
	}
	if n.Resource == "referencegrants" {
		missingGrant(c, n, f)
		return f
	}
	f.Next = []Step{{Label: "kubectl get", Command: listCommand(n)}}
	return f
}

// missingGrant explains a ReferenceGrant nobody has created. Its node has
// no real name: it stands for the grant that routes from other namespaces
// would need to reach the Services here, so the finding names them.
func missingGrant(c *Context, n *graph.Node, f *Finding) {
	var needs []string
	in := c.G.In(n.ID) // one edge per route that needs it
	for _, e := range in {
		route := c.Node(e.From)
		var services []string
		for _, out := range c.G.Out(e.From) {
			if svc := c.Node(out.To); out.Type == graph.RoutesTo && svc.Namespace == n.Namespace {
				services = append(services, "Service "+svc.Name)
			}
		}
		need := fmt.Sprintf("%s %s/%s sends traffic to %s", route.Kind, route.Namespace, route.Name, list(services))
		needs = append(needs, need+" ("+strings.Join(e.Via, ", ")+")")
		f.Title = "No ReferenceGrant in " + n.Namespace + " lets " + strings.Replace(need, "sends traffic to", "reach", 1)
	}
	if len(in) > 1 {
		f.Title = "No ReferenceGrant in " + n.Namespace + " lets routes from other namespaces reach its Services"
	}
	f.Detail = strings.Join(needs, "; ") + ". Kubernetes lets a route reach a Service in another namespace only when a " +
		"ReferenceGrant in that namespace allows routes from the route's namespace."
	f.Evidence = []Evidence{{Source: "reference", Text: "Opscope read every ReferenceGrant in " + n.Namespace + " and none allows it."}}
	f.Next = []Step{{Label: "kubectl get", Command: "kubectl get referencegrants.gateway.networking.k8s.io -n " + n.Namespace}}
}

// unknown is the finding an object of a kind Opscope can't read gets.
func unknown(c *Context, n *graph.Node) *Finding {
	return &Finding{
		Rule:       "unknown",
		Severity:   Info,
		Confidence: Possible,
		Title:      fmt.Sprintf("Opscope can't check %s %s", n.Kind, qualified(n)),
		Detail:     n.Message,
	}
}

// qualified writes "namespace/name", or just the name of a cluster-wide object.
func qualified(n *graph.Node) string {
	if n.Namespace == "" {
		return n.Name
	}
	return n.Namespace + "/" + n.Name
}

// listCommand lists the objects of a kind in a namespace, to see what is
// there instead of the missing one.
func listCommand(n *graph.Node) string {
	command := "kubectl get " + kubectlResource(n.Resource)
	if n.Namespace != "" {
		command += " -n " + n.Namespace
	}
	return command
}

// unexplained is the finding an unhealthy object gets when no rule explains
// it: what it says about itself, its recent warnings, and its unhealthy
// neighbours as possibly related. It's a guess, and says so.
func unexplained(c *Context, n *graph.Node) *Finding {
	word := map[graph.State]string{graph.Failing: "failing", graph.Degraded: "not fully working"}[n.State]
	f := &Finding{
		Rule:       "unexplained",
		Severity:   graph2severity(n.State),
		Confidence: Possible,
		Title:      fmt.Sprintf("Opscope can't tell why it's %s", word),
		Detail:     "No rule explains this yet. Its status and events may say more.",
		Evidence:   []Evidence{{Source: "status", Text: strings.TrimSpace(n.Status + " " + n.Message)}},
		Next:       []Step{eventsOf(n), kubectl("describe", n)},
	}
	if obj := c.Object(n); obj != nil {
		f.Evidence = append(f.Evidence, eventEvidence(c.Events(obj.GetUID()))...)
	}
	seen := map[string]bool{}
	for _, e := range append(append([]*graph.Edge{}, c.G.Out(n.ID)...), c.G.In(n.ID)...) {
		for _, id := range []string{e.From, e.To} {
			if other := c.Node(id); id != n.ID && !seen[id] && other.State != graph.OK && other.State != graph.Progressing {
				seen[id] = true
				f.Related = append(f.Related, id)
			}
		}
	}
	sort.Strings(f.Related)
	if len(f.Related) > 5 {
		f.Related = f.Related[:5]
	}
	return f
}
