package diagnose

import (
	"fmt"
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
	serviceSelector,
	serviceEndpoints,
	routeBackend,
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
		// The node stands for a grant nobody has created; it has no real name.
		f.Title = "No ReferenceGrant in " + n.Namespace + " allows it"
		f.Detail = "A route in another namespace sends traffic to a Service in " + n.Namespace +
			". Kubernetes allows that only when a ReferenceGrant in " + n.Namespace + " permits routes from that namespace."
		f.Next = []Step{{Label: "kubectl get", Command: "kubectl get referencegrants.gateway.networking.k8s.io -n " + n.Namespace}}
		return f
	}
	f.Next = []Step{{Label: "kubectl get", Command: listCommand(n)}}
	return f
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
