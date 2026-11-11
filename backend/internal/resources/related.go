package resources

// What a detail page knows from the graph: whether the objects it links
// to exist, and which objects are connected to it. Reading them from the
// graph means a detail page and the Topology page can never disagree.

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Relations is the graph as a detail page asks it (internal/graph
// implements it). Edge kinds are the graph's: "references", "binds",
// "runs-on", "owns", ...
type Relations interface {
	// Presence says whether an object exists: "present", "missing" or
	// "unknown"; "" when the graph has no word on it (a kind it doesn't
	// hold, say).
	Presence(resource, namespace, name string) string
	// Related lists the objects joined to one by edges of a kind: what it
	// points to (out) or what points to it (in), sorted by kind and name.
	Related(resource, namespace, name, edgeKind string, out bool) []Related
}

// Related is one object at the other end of an edge.
type Related struct {
	Resource, Kind, Namespace, Name string
	Presence                        string
	Status                          string   // what its list page shows
	Fields                          []string // the fields the edge was read from
	Notes                           []string // what each field is: "env PASSWORD", "volume config"
}

// AddRelations completes a detail page from the graph:
//   - a link to something that doesn't exist says "(not found)" instead,
//     rather than leading to an error page;
//   - a claim lists the pods using it, a StatefulSet the claims made from
//     its templates, and a StorageClass counts its volumes and claims;
//   - a ConfigMap or Secret lists what uses it ("Used by").
//
// src is only read for a StatefulSet's claims' capacity, which the graph
// doesn't hold.
func AddRelations(ctx context.Context, src Source, d *Detail, resource string, rel Relations) {
	markMissing(d, rel)
	switch resource {
	case "persistentvolumeclaims":
		claimUsers(d, rel)
	case "statefulsets":
		statefulSetClaims(ctx, src, d, rel)
	case "storageclasses":
		storageClassUsage(d, rel)
	case "configmaps", "secrets":
		usedBy(d, resource, rel)
	}
}

func missing(rel Relations, l *Link) bool {
	return l != nil && rel.Presence(l.Resource, l.Namespace, l.Name) == "missing"
}

// markMissing turns every link to an object that doesn't exist into text
// that says so: a field, a table cell, an owner.
func markMissing(d *Detail, rel Relations) {
	for i := range d.Fields {
		if f := &d.Fields[i]; missing(rel, f.Link) {
			f.Value += " (not found)"
			f.Link = nil
		}
	}
	for t := range d.Tables {
		table := &d.Tables[t]
		for r := range table.Links {
			for c, l := range table.Links[r] {
				if missing(rel, l) {
					table.Rows[r][c] += " (not found)"
					table.Links[r][c] = nil
				}
			}
		}
	}
	for i := range d.Owners {
		if o := &d.Owners[i]; o.Resource != "" && missing(rel, &Link{Resource: o.Resource, Namespace: d.Namespace, Name: o.Name}) {
			o.Resource = "" // no page to open
			o.Name += " (not found)"
		}
	}
}

// linkTo links a related object when Opscope has a page for its kind.
func linkTo(r Related) *Link {
	if _, ok := kinds[r.Resource]; !ok || r.Presence != "present" {
		return nil
	}
	return &Link{Resource: r.Resource, Namespace: r.Namespace, Name: r.Name}
}

func only(list []Related, resource string) []Related {
	return slices.DeleteFunc(slices.Clone(list), func(r Related) bool { return r.Resource != resource })
}

// claimUsers lists the pods that use a claim, with their status and node.
func claimUsers(d *Detail, rel Relations) {
	t := Table{Title: "Used by", Columns: []string{"Pod", "Status", "Node"}}
	for _, pod := range only(rel.Related("persistentvolumeclaims", d.Namespace, d.Name, "references", false), "pods") {
		node := ""
		if nodes := rel.Related("pods", pod.Namespace, pod.Name, "runs-on", true); len(nodes) > 0 {
			node = nodes[0].Name
		}
		t.addRow(Cell{Text: pod.Name, Link: linkTo(pod)}, text(pod.Status), text(node))
	}
	d.addTable(t)
}

// statefulSetClaims lists the claims made from a StatefulSet's templates,
// which outlive its pods.
func statefulSetClaims(ctx context.Context, src Source, d *Detail, rel Relations) {
	hasTemplates := slices.ContainsFunc(d.Tables, func(t Table) bool { return t.Title == "Volume claim templates" && len(t.Rows) > 0 })
	if !hasTemplates {
		return
	}
	// The capacity is the claim's own, which the graph doesn't keep. It's
	// left blank if the claims can't be read.
	capacity := map[string]string{}
	if claims, err := ListOf[*corev1.PersistentVolumeClaim](ctx, src, "persistentvolumeclaims", Query{Namespace: d.Namespace}); err == nil {
		for _, pvc := range claims {
			if size, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
				capacity[pvc.Name] = sizeText(size.Value())
			}
		}
	}
	t := Table{Title: "Claims", Columns: []string{"Claim", "Status", "Capacity", "Volume"}}
	for _, claim := range only(rel.Related("statefulsets", d.Namespace, d.Name, "references", true), "persistentvolumeclaims") {
		if !slices.ContainsFunc(claim.Fields, func(f string) bool { return strings.HasPrefix(f, "spec.volumeClaimTemplates") }) {
			continue // a claim its pod template uses directly
		}
		volume := Cell{}
		if volumes := only(rel.Related("persistentvolumeclaims", claim.Namespace, claim.Name, "binds", true), "persistentvolumes"); len(volumes) > 0 {
			volume = Cell{Text: volumes[0].Name, Link: linkTo(volumes[0])}
		}
		t.addRow(Cell{Text: claim.Name, Link: linkTo(claim)}, text(claim.Status), text(capacity[claim.Name]), volume)
	}
	d.addTable(t)
}

// storageClassUsage counts the volumes and claims of a StorageClass.
func storageClassUsage(d *Detail, rel Relations) {
	bound := rel.Related("storageclasses", "", d.Name, "binds", false)
	d.add("Volumes", strconv.Itoa(len(only(bound, "persistentvolumes"))))
	d.add("Claims", strconv.Itoa(len(only(bound, "persistentvolumeclaims"))))
}

// usedBy lists what uses a ConfigMap or Secret: workloads first, each with
// how many of its pods use it, then pods no listed workload owns, then
// anything else (an Ingress's TLS, a Gateway's certificate). How each one
// uses it comes from the fields the edges were read from.
func usedBy(d *Detail, resource string, rel Relations) {
	users := rel.Related(resource, d.Namespace, d.Name, "references", false)
	isWorkload := map[string]bool{"deployments": true, "statefulsets": true, "daemonsets": true, "jobs": true, "cronjobs": true}
	listed := map[string]int{} // workload "resource/name" → its pods counted
	for _, u := range users {
		if isWorkload[u.Resource] {
			listed[u.Resource+"/"+u.Name] = 0
		}
	}
	// ownersOf follows a pod's owners, up to three steps, and returns every
	// listed workload among them: a Deployment owns a ReplicaSet that owns
	// the pod; a CronJob, a Job that owns it, and both may be listed.
	var ownersOf func(resource, name string, depth int) []string
	ownersOf = func(resource, name string, depth int) []string {
		var found []string
		for _, o := range rel.Related(resource, d.Namespace, name, "owns", false) {
			key := o.Resource + "/" + o.Name
			if _, ok := listed[key]; ok {
				found = append(found, key)
			}
			if depth < 2 {
				found = append(found, ownersOf(o.Resource, o.Name, depth+1)...)
			}
		}
		return found
	}
	var rows []Related
	for _, u := range users {
		if u.Resource == "pods" {
			if owners := ownersOf("pods", u.Name, 0); len(owners) > 0 {
				for _, key := range owners {
					listed[key]++
				}
				continue
			}
		}
		rows = append(rows, u)
	}
	rank := func(r Related) int {
		switch {
		case isWorkload[r.Resource]:
			return 0
		case r.Resource == "pods":
			return 1
		}
		return 2
	}
	slices.SortStableFunc(rows, func(a, b Related) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})

	t := Table{Title: "Used by", Columns: []string{"Kind", "Name", "How", "Pods"}}
	for _, r := range rows {
		pods := ""
		if isWorkload[r.Resource] {
			pods = strconv.Itoa(listed[r.Resource+"/"+r.Name])
		}
		t.addRow(text(r.Kind), Cell{Text: r.Name, Link: linkTo(r)}, text(howUsed(r.Notes)), text(pods))
	}
	d.addTable(t)
}

// howUsed joins the notes of the fields a reference was read from, each
// once: "env PASSWORD, volume creds".
func howUsed(notes []string) string {
	var out []string
	for _, n := range notes {
		if n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return strings.Join(out, ", ")
}
