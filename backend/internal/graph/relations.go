package graph

import (
	"cmp"
	"slices"

	"opscope/internal/resources"
)

// Relations is the graph as detail pages ask it (resources.AddRelations).
// Use it only while the graph is being read (inside Engine.Read).
func (g *Graph) Relations() resources.Relations {
	return relations{g}
}

type relations struct{ g *Graph }

// Presence of an object. One the graph has no node for is missing when the
// graph reads its kind (nothing refers to it, and it isn't there), and
// unknown to the graph otherwise.
func (r relations) Presence(resource, namespace, name string) string {
	n, err := r.g.Find(resource, namespace, name, Query{})
	switch {
	case err == nil:
		return n.Presence
	case r.g.Readable(resource):
		return Missing
	}
	return ""
}

func (r relations) Related(resource, namespace, name, edgeKind string, out bool) []resources.Related {
	n, err := r.g.Find(resource, namespace, name, Query{})
	if err != nil {
		return nil
	}
	index := r.g.in
	if out {
		index = r.g.out
	}
	var list []resources.Related
	for edgeID := range index[n.ID] {
		es := r.g.edges[edgeID]
		if es.kind != edgeKind {
			continue
		}
		other := es.from
		if out {
			other = es.to
		}
		o := r.g.nodes[other]
		rel := resources.Related{
			Resource: o.Resource, Kind: o.Kind, Namespace: o.Namespace, Name: o.Name,
			Presence: o.Presence, Status: o.Status,
		}
		for _, ev := range r.g.edge(edgeID).Evidence {
			rel.Fields = append(rel.Fields, ev.Field)
			rel.Notes = append(rel.Notes, ev.Note)
		}
		list = append(list, rel)
	}
	slices.SortFunc(list, func(a, b resources.Related) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})
	return list
}
