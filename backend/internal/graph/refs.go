package graph

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/resources"
)

// Builder reads one kind of object and says what it refers to. Builders
// are a registry (contract 7): adding an edge is one more entry, never a
// new case in a switch.
type Builder struct {
	Name     string // "pods.volumes"; shown in every piece of evidence it gives
	Resource string // the kind it reads, e.g. "pods"; "" for every kind
	Refs     func(obj runtime.Object, r *Refs)
}

var builders []Builder

// Register adds a builder. It's called from init functions, before any
// graph is built.
func Register(b Builder) {
	builders = append(builders, b)
}

// buildersFor returns the builders that read a kind, in registration order.
func buildersFor(resource string) []Builder {
	var out []Builder
	for _, b := range builders {
		if b.Resource == "" || b.Resource == resource {
			out = append(out, b)
		}
	}
	return out
}

// Ref is one reference an object makes: to a named object (Target) or to
// every object a selector picks (Match).
type Ref struct {
	Builder  string
	Kind     string // the edge kind
	Field    string
	Note     string
	Optional bool

	// Reverse makes the target the dependent: an owner reference is read
	// from the owned object, but the edge goes owner → owned.
	Reverse bool

	Target *Target
	Match  *Match
}

// Target names one object, as references in Kubernetes do.
type Target struct {
	Resource  string // URL name; "" for a kind Opscope doesn't know
	Group     string // only needed when Resource is ""
	Kind      string
	Namespace string // "" for cluster-wide kinds
	Name      string
	UID       string // set when the reference has it (owner references)
}

// key is how a target, or an object, is found by name: the resource, the
// namespace when there is one, and the name. A kind Opscope doesn't know is
// keyed by its kind and group instead.
func (t Target) key() string {
	resource := t.Resource
	if resource == "" {
		resource = t.Kind
		if t.Group != "" {
			resource += "." + t.Group
		}
	}
	return objectKey(resource, t.Namespace, t.Name)
}

func objectKey(resource, namespace, name string) string {
	if namespace == "" {
		return resource + "/" + name
	}
	return resource + "/" + namespace + "/" + name
}

// Match picks every object of one kind in one namespace that Selects
// accepts: a Service's pods, a StatefulSet's claims.
type Match struct {
	Resource  string
	Namespace string
	// Selects may read only an object's name and labels: matches are
	// checked again when objects come and go or their labels change.
	Selects func(obj runtime.Object) bool
	// Dangling, when set, is recorded on the node if nothing matches
	// ("matches no pods").
	Dangling string
}

// Refs collects one object's references for its builders.
type Refs struct {
	builder string
	list    []Ref
}

func (r *Refs) add(ref Ref) {
	ref.Builder = r.builder
	r.list = append(r.list, ref)
}

// To adds a reference by name to an object of a kind Opscope knows.
// A cluster-wide kind ignores namespace.
func (r *Refs) To(edgeKind, resource, namespace, name, field, note string, optional bool) {
	if name == "" {
		return
	}
	k, ok := resources.Lookup(resource)
	if !ok {
		panic(fmt.Sprintf("graph: builder %s refers to %q, which isn't a known kind", r.builder, resource))
	}
	if !k.Namespaced {
		namespace = ""
	}
	r.add(Ref{
		Kind: edgeKind, Field: field, Note: note, Optional: optional,
		Target: &Target{Resource: resource, Kind: k.KindName, Namespace: namespace, Name: name},
	})
}

// ToKind adds a reference that names its target's API group and kind, as
// owner references and Gateway API references do. A kind Opscope doesn't
// know becomes an unknown node.
func (r *Refs) ToKind(edgeKind, group, kind, namespace, name, uid, field, note string, reverse bool) {
	if name == "" {
		return
	}
	t := Target{Group: group, Kind: kind, Namespace: namespace, Name: name, UID: uid}
	if k, ok := resources.LookupKind(group, kind); ok {
		t.Resource, t.Group = k.Resource, ""
		if !k.Namespaced {
			t.Namespace = ""
		}
	}
	r.add(Ref{Kind: edgeKind, Field: field, Note: note, Reverse: reverse, Target: &t})
}

// Matching adds a reference to every object Match picks.
func (r *Refs) Matching(edgeKind string, m Match, field, note string) {
	r.add(Ref{Kind: edgeKind, Field: field, Note: note, Match: &m})
}
