package resources

// ReplicaSets: their own detail page, and a Deployment's ReplicaSets shown
// as its revisions. There's no ReplicaSet page in the sidebar; a Deployment
// keeps up to ten old ones, and its page is where people look for them.

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func replicaSetSections(d *Detail, obj *unstructured.Unstructured) error {
	var rs appsv1.ReplicaSet
	if err := convert(obj, &rs); err != nil {
		return err
	}
	if n := revisionOf(rs.ObjectMeta); n > 0 {
		d.add("Revision", strconv.Itoa(n))
	}
	d.add("Replicas", fmt.Sprintf("%d desired, %d current, %d ready, %d available",
		replicas(rs.Spec.Replicas), rs.Status.Replicas, rs.Status.ReadyReplicas, rs.Status.AvailableReplicas))
	d.add("Selector", selectorText(rs.Spec.Selector))
	// The object is read from the cluster, so its template is whole here,
	// though the cache drops it: these are the containers of this revision.
	d.Containers = templateContainers(rs.Spec.Template.Spec)
	return nil
}

// deploymentRevisions lists the ReplicaSets a Deployment owns, newest
// revision first, marking the one it runs now. Read from the cache: the
// revision numbers are annotations, which the graph doesn't hold.
func deploymentRevisions(ctx context.Context, src Source, d *Detail) {
	items, err := ListOf[*appsv1.ReplicaSet](ctx, src, "replicasets", Query{Namespace: d.Namespace})
	if err != nil {
		return // left out, like the rest of what the graph and cache add
	}
	current := d.Annotations[revisionAnnotation]
	var owned []*appsv1.ReplicaSet
	for _, rs := range items {
		for _, o := range rs.OwnerReferences {
			if string(o.UID) == d.UID && o.Controller != nil && *o.Controller {
				owned = append(owned, rs)
			}
		}
	}
	slices.SortFunc(owned, func(a, b *appsv1.ReplicaSet) int {
		return cmp.Or(cmp.Compare(revisionOf(b.ObjectMeta), revisionOf(a.ObjectMeta)), cmp.Compare(a.Name, b.Name))
	})

	t := Table{Title: "Revisions", Columns: []string{"Revision", "ReplicaSet", "Pods", "State", "Age"}}
	now := time.Now()
	for _, rs := range owned {
		revision, state := "", "old"
		if n := revisionOf(rs.ObjectMeta); n > 0 {
			revision = strconv.Itoa(n)
		}
		switch {
		case revision != "" && revision == current:
			state = "current"
		case rs.Status.Replicas > 0:
			state = "old, still running" // a rollout in progress, or stuck
		}
		t.addRow(text(revision),
			Cell{Text: rs.Name, Link: &Link{Resource: "replicasets", Namespace: rs.Namespace, Name: rs.Name}},
			text(fmt.Sprintf("%d/%d", rs.Status.ReadyReplicas, replicas(rs.Spec.Replicas))),
			text(state),
			text(ageText(rs.CreationTimestamp.Time, now)))
	}
	d.addTable(t)
}

// ageText is how long ago t was, in one unit as kubectl says it: "45s",
// "12m", "5h", "3d". "" when t is unset.
func ageText(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := max(now.Sub(t), 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// ownedPods lists the pods an owner (a ReplicaSet) has, from the graph,
// with their status and node.
func ownedPods(d *Detail, resource string, rel Relations) {
	t := Table{Title: "Pods", Columns: []string{"Pod", "Status", "Node"}}
	for _, pod := range only(rel.Related(resource, d.Namespace, d.Name, "owns", true), "pods") {
		t.addRow(Cell{Text: pod.Name, Link: linkTo(pod)}, text(pod.Status), text(nodeOf(pod, rel)))
	}
	d.addTable(t)
}

// nodeOf is the node a pod runs on, from the graph; "" before it's scheduled.
func nodeOf(pod Related, rel Relations) string {
	if nodes := rel.Related("pods", pod.Namespace, pod.Name, "runs-on", true); len(nodes) > 0 {
		return nodes[0].Name
	}
	return ""
}
