package diagnose

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"opscope/internal/graph"
	"opscope/internal/kube"
	"opscope/internal/snapshot"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Rule looks at one object and returns what's wrong with it.
type Rule struct {
	ID        string
	Resources []string // the kinds it looks at, by resource name
	Check     func(c *Context, n *graph.Node) []*Finding
}

// Context is what rules can read.
type Context struct {
	S   *snapshot.Snapshot
	G   *graph.Graph
	Now time.Time // the snapshot's time
}

// Object returns the snapshot's object behind a node, or nil.
func (c *Context) Object(n *graph.Node) metav1.Object {
	obj, ok := c.S.Get(n.Resource, n.Namespace, n.Name)
	if !ok {
		return nil
	}
	return obj
}

// Node returns a graph node by ID, or nil.
func (c *Context) Node(id string) *graph.Node {
	n, _ := c.G.Node(id)
	return n
}

// topOwner follows owns edges up as far as they go: a pod's Deployment, a
// Job's CronJob.
func topOwner(g *graph.Graph, id string) string {
	for range maxDepth {
		owner := ""
		for _, e := range g.In(id) {
			if e.Type == graph.Owns {
				owner = e.From
			}
		}
		if owner == "" {
			return id
		}
		id = owner
	}
	return id
}

// Events returns an object's Warning events with one of the given reasons
// (any, when none are given), newest first.
func (c *Context) Events(uid types.UID, reasons ...string) []*corev1.Event {
	var events []*corev1.Event
	for _, e := range c.S.EventsFor(uid) {
		if len(reasons) == 0 || slices.Contains(reasons, e.Reason) {
			events = append(events, e)
		}
	}
	return events
}

// eventEvidence turns events into evidence, newest first, at most three.
func eventEvidence(events []*corev1.Event) []Evidence {
	var evidence []Evidence
	for i, e := range events {
		if i == 3 {
			break
		}
		at := kube.EventLastSeen(e)
		evidence = append(evidence, Evidence{Source: "event", Text: e.Reason + ": " + e.Message, Time: &at, Count: kube.EventCount(e)})
	}
	return evidence
}

func conditionEvidence(t, status, reason, message string, at metav1.Time) Evidence {
	text := fmt.Sprintf("%s=%s", t, status)
	if reason != "" {
		text += " (" + reason + ")"
	}
	if message != "" {
		text += ": " + message
	}
	e := Evidence{Source: "condition", Text: text}
	if !at.IsZero() {
		e.Time = &at.Time
	}
	return e
}

// ---- Next steps ----

// The only kubectl verbs a step may suggest. Opscope never offers a command
// that changes the cluster, not even as text to copy.
var readOnlyVerbs = []string{"get", "describe", "logs"}

// kubectl writes a read-only kubectl command about one object.
func kubectl(verb string, n *graph.Node, extra ...string) Step {
	args := []string{"kubectl", verb, kubectlResource(n.Resource), n.Name}
	if verb == "logs" {
		args = []string{"kubectl", "logs", n.Name}
	}
	if n.Namespace != "" {
		args = append(args, "-n", n.Namespace)
	}
	args = append(args, extra...)
	return Step{Label: "kubectl " + verb, Command: strings.Join(args, " ")}
}

// kubectlResource is how kubectl names a resource. Gateway API kinds need
// their group: other projects have a kind called Gateway too.
func kubectlResource(resource string) string {
	switch resource {
	case "gateways", "httproutes", "gatewayclasses", "referencegrants":
		return resource + ".gateway.networking.k8s.io"
	}
	return resource
}

// eventsOf suggests listing an object's events, which have more than the
// three a finding shows.
func eventsOf(n *graph.Node) Step {
	return Step{Label: "Events", Object: n.ID, Tab: "events"}
}

// ---- Wording ----

// describe names a node for a sentence: "Secret stripe-key", or with its
// namespace when that's not the one we're talking about.
func describe(n *graph.Node, from string) string {
	if n == nil {
		return "it"
	}
	if n.Namespace != "" && n.Namespace != from {
		return n.Kind + " " + n.Namespace + "/" + n.Name
	}
	return n.Kind + " " + n.Name
}

// ago writes a duration the way people say it: "45s", "12m", "3h20m", "4d".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		if m := int(d.Minutes()) % 60; m != 0 {
			return fmt.Sprintf("%dh%dm", int(d.Hours()), m)
		}
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// list joins words as a sentence does: "a", "a and b", "a, b and c".
func list(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
