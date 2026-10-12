package diagnose

import (
	"fmt"

	"opscope/internal/graph"

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
		since := ago(c.Now.Sub(cond.LastHeartbeatTime.Time))
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

// ---- node.pressure: a node is short of something, or cordoned ----

var pressures = map[corev1.NodeConditionType]string{
	corev1.NodeMemoryPressure: "memory",
	corev1.NodeDiskPressure:   "disk space",
	corev1.NodePIDPressure:    "process IDs",
}

var nodePressure = Rule{ID: "node.pressure", Resources: []string{"nodes"}, Check: func(c *Context, n *graph.Node) []*Finding {
	node, _ := c.Object(n).(*corev1.Node)
	if node == nil || n.State == graph.Failing {
		return nil // not Ready says more
	}
	var findings []*Finding
	for _, cond := range node.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}
		evidence := []Evidence{conditionEvidence(string(cond.Type), "True", cond.Reason, cond.Message, cond.LastTransitionTime)}
		if what, ok := pressures[cond.Type]; ok {
			findings = append(findings, &Finding{
				Severity:   Degraded,
				Confidence: Certain,
				Title:      fmt.Sprintf("Node %s is low on %s", n.Name, what),
				Detail:     fmt.Sprintf("While it's low on %s, the kubelet evicts pods to free some up, and new pods that need it aren't scheduled here.", what),
				Evidence:   evidence,
				Next:       []Step{kubectl("describe", n)},
			})
		}
		if cond.Type == corev1.NodeNetworkUnavailable {
			findings = append(findings, &Finding{
				Severity:   Failing,
				Confidence: Certain,
				Title:      fmt.Sprintf("Node %s's network isn't set up", n.Name),
				Detail:     "Its network plugin hasn't configured it, so pods on it can't reach the network.",
				Evidence:   evidence,
				Next:       []Step{kubectl("describe", n)},
			})
		}
	}
	if node.Spec.Unschedulable {
		findings = append(findings, &Finding{
			Severity:   Info,
			Confidence: Certain,
			Title:      fmt.Sprintf("Node %s is cordoned", n.Name),
			Detail:     "No new pods are scheduled on it; the ones there keep running. It's usually done on purpose, before maintenance.",
			Evidence:   []Evidence{{Source: "spec", Text: "unschedulable: true"}},
		})
	}
	return findings
}}
