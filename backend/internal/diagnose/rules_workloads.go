package diagnose

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"opscope/internal/graph"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// ---- node.not-ready: a node isn't Ready ----

var nodeNotReady = Rule{ID: "node.not-ready", Resources: []string{"nodes"}, Check: func(c *Context, n *graph.Node) []*Finding {
	node, _ := c.Object(n).(*corev1.Node)
	if node == nil || n.State != graph.Failing {
		return nil
	}
	f := &Finding{
		Severity:   Failing,
		Confidence: Certain,
		Title:      fmt.Sprintf("Node %s isn't Ready", n.Name),
		Detail:     "Its kubelet has never reported that the node is ready.",
		Next:       []Step{kubectl("describe", n)},
	}
	for _, cond := range node.Status.Conditions {
		if cond.Type != corev1.NodeReady {
			continue
		}
		f.Evidence = append(f.Evidence, conditionEvidence("Ready", string(cond.Status), cond.Reason, cond.Message, cond.LastTransitionTime))
		since := c.Now.Sub(cond.LastHeartbeatTime.Time).Round(time.Minute)
		switch cond.Status {
		case corev1.ConditionUnknown:
			f.Title = fmt.Sprintf("Node %s stopped reporting", n.Name)
			f.Detail = fmt.Sprintf("Its kubelet last reported %s ago. The machine may be off, out of the network, or its kubelet stopped.", since)
		default:
			f.Detail = fmt.Sprintf("Its kubelet says it isn't ready: %s", orElse(cond.Message, cond.Reason))
		}
	}
	return []*Finding{f}
}}

// ---- workload.unavailable: fewer replicas ready than wanted ----

var workloadUnavailable = Rule{ID: "workload.unavailable", Resources: []string{"deployments", "statefulsets", "daemonsets"}, Check: func(c *Context, n *graph.Node) []*Finding {
	if n.State == graph.OK {
		return nil
	}
	var ready, desired int32
	var evidence []Evidence
	switch w := c.Object(n).(type) {
	case *appsv1.Deployment:
		ready, desired = w.Status.ReadyReplicas, replicaCount(w.Spec.Replicas)
		for _, cond := range w.Status.Conditions {
			if cond.Status != corev1.ConditionTrue || cond.Reason == "ProgressDeadlineExceeded" {
				evidence = append(evidence, conditionEvidence(string(cond.Type), string(cond.Status), cond.Reason, cond.Message, cond.LastTransitionTime))
			}
		}
	case *appsv1.StatefulSet:
		ready, desired = w.Status.ReadyReplicas, replicaCount(w.Spec.Replicas)
	case *appsv1.DaemonSet:
		ready, desired = w.Status.NumberReady, w.Status.DesiredNumberScheduled
	default:
		return nil
	}

	// Still starting or rolling out is worth knowing, not a fault.
	f := &Finding{Severity: Info, Confidence: Certain, Evidence: evidence, Next: []Step{eventsOf(n)}}
	if n.State == graph.Failing || n.State == graph.Degraded {
		f.Severity = graph2severity(n.State)
	}

	// What's behind it: its pods that aren't OK, and ReplicaSets that can't
	// create any.
	for _, owned := range ownedTree(c, n.ID) {
		switch {
		case owned.Resource == "pods" && owned.State != graph.OK:
			f.Blames = append(f.Blames, owned.ID)
		case owned.Resource == "replicasets" && owned.State == graph.Failing && !hasPods(c, owned.ID):
			f.Blames = append(f.Blames, owned.ID)
		}
	}

	switch {
	case ready >= desired:
		f.Title = "It's rolling out: some replicas aren't updated yet"
	case ready == 0 && desired == 1:
		f.Title = "Its only replica isn't ready"
	case ready == 0:
		f.Title = fmt.Sprintf("None of its %d replicas are ready", desired)
	default:
		f.Title = fmt.Sprintf("%d of its %d replicas aren't ready", desired-ready, desired)
	}
	switch {
	case f.Severity == Info:
		f.Detail = "It's still getting there: pods are starting or being replaced."
	case len(f.Blames) == 0:
		f.Detail = n.Message
	}
	return []*Finding{f}
}}

// graph2severity is the severity of a finding about an object in a state.
func graph2severity(s graph.State) Severity {
	if s == graph.Degraded {
		return Degraded
	}
	return Failing
}

// ownedTree lists what an object owns, and what that owns.
func ownedTree(c *Context, id string) []*graph.Node {
	var nodes []*graph.Node
	for _, e := range c.G.Out(id) {
		if e.Type == graph.Owns {
			owned := c.Node(e.To)
			nodes = append(nodes, owned)
			nodes = append(nodes, ownedTree(c, owned.ID)...)
		}
	}
	return nodes
}

func hasPods(c *Context, id string) bool {
	for _, e := range c.G.Out(id) {
		if e.Type == graph.Owns && c.Node(e.To).Resource == "pods" {
			return true
		}
	}
	return false
}

func replicaCount(n *int32) int32 {
	if n == nil {
		return 1
	}
	return *n
}

// ---- create-failed: Kubernetes refuses to create a workload's pods ----

var (
	exceededQuotaRE = regexp.MustCompile(`exceeded quota: ([^,\s]+), requested: (.+?), used: (.+?), limited: ([^\s]+)`)
	mustSpecifyRE   = regexp.MustCompile(`failed quota: ([^:\s]+): must specify (.+)$`)
	accountRE       = regexp.MustCompile(`serviceaccount "([^"]+)" not found`)
	podSecurityRE   = regexp.MustCompile(`violates PodSecurity "([^"]+)": (.+)$`)
	limitRangeRE    = regexp.MustCompile(`(minimum|maximum) (\S+) usage per (Container|Pod) is (\S+), but (limit|request) is (\S+)`)
)

var createFailed = Rule{ID: "create-failed", Resources: []string{"replicasets", "statefulsets", "daemonsets", "jobs"}, Check: func(c *Context, n *graph.Node) []*Finding {
	obj := c.Object(n)
	if obj == nil || n.State == graph.OK {
		return nil
	}

	// A ReplicaSet says it in a condition; every kind says it in events.
	message := ""
	var evidence []Evidence
	if rs, ok := obj.(*appsv1.ReplicaSet); ok {
		for _, cond := range rs.Status.Conditions {
			if cond.Type == appsv1.ReplicaSetReplicaFailure && cond.Status == corev1.ConditionTrue {
				message = cond.Message
				evidence = append(evidence, conditionEvidence("ReplicaFailure", "True", cond.Reason, cond.Message, cond.LastTransitionTime))
			}
		}
	}
	events := c.Events(obj.GetUID(), "FailedCreate")
	if message == "" && len(events) > 0 {
		message = events[0].Message
	}
	if message == "" {
		return nil
	}
	evidence = append(evidence, eventEvidence(events)...)

	f := &Finding{
		Severity:   Failing,
		Confidence: Certain,
		Evidence:   evidence,
		Next:       []Step{eventsOf(n)},
	}
	in := func(resource, name string) string {
		return snapshot.Key{Resource: resource, Namespace: n.Namespace, Name: name}.String()
	}
	blame := func(id string) {
		if c.Node(id) != nil {
			f.Blames = append(f.Blames, id)
		}
	}

	if m := exceededQuotaRE.FindStringSubmatch(message); m != nil {
		f.Title = fmt.Sprintf("ResourceQuota %s doesn't leave room for its pods", m[1])
		f.Detail = quotaDetail(m[2], m[3], m[4])
		blame(in("resourcequotas", m[1]))
	} else if m := mustSpecifyRE.FindStringSubmatch(message); m != nil {
		f.Title = fmt.Sprintf("ResourceQuota %s requires every pod to set %s", m[1], strings.TrimSuffix(m[2], "."))
		f.Detail = "A quota on a resource means every pod in the namespace has to state how much of it it uses. Its pods don't."
		blame(in("resourcequotas", m[1]))
	} else if m := accountRE.FindStringSubmatch(message); m != nil {
		f.Title = fmt.Sprintf("Its pods need ServiceAccount %s", m[1])
		f.Detail = "Kubernetes won't create a pod whose service account doesn't exist."
		blame(in("serviceaccounts", m[1]))
	} else if m := podSecurityRE.FindStringSubmatch(message); m != nil {
		f.Title = fmt.Sprintf("Namespace %s's Pod Security level rejects its pods", n.Namespace)
		f.Detail = fmt.Sprintf("The namespace enforces the %q Pod Security Standard, and its pods break it: %s", m[1], m[2])
		blame(snapshot.Key{Resource: "namespaces", Name: n.Namespace}.String())
	} else if m := limitRangeRE.FindStringSubmatch(message); m != nil {
		f.Title = "A LimitRange in " + n.Namespace + " rejects its pods"
		f.Detail = fmt.Sprintf("The %s %s per %s is %s, and its pods ask for %s.", m[1], m[2], strings.ToLower(m[3]), m[4], m[6])
	} else {
		f.Title = "Kubernetes refuses to create its pods"
		f.Detail = message
		f.Confidence = Likely
	}
	return []*Finding{f}
}}

// quotaDetail explains an exceeded quota. The API server writes each part
// as "limits.memory=2Gi"; when they're about one resource, it's said once.
func quotaDetail(requested, used, limited string) string {
	name, asked, ok := strings.Cut(requested, "=")
	usedName, usedAmount, _ := strings.Cut(used, "=")
	limitName, limit, _ := strings.Cut(limited, "=")
	if !ok || strings.Contains(requested, ",") || usedName != name || limitName != name {
		return fmt.Sprintf("Each pod asks for %s. The namespace has used %s, and the quota allows %s. Free some up, raise the quota, or ask for less.", requested, used, limited)
	}
	return fmt.Sprintf("Each pod asks for %s of %s, and the namespace has used %s of the %s its quota allows. Free some up, raise the quota, or ask for less.",
		asked, name, usedAmount, limit)
}
