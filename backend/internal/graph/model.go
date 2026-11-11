// Package graph is the Graph engine: a model of one cluster's objects and
// the typed edges between them, each edge with the fields it was read from.
// See docs/design/topology.md.
//
// Edges point from the dependent to its dependency, "A needs B": a pod
// needs its Secret and its node, a Service needs the pods it selects.
// Following edges forward gives what an object depends on (upstream);
// following them backward gives what depends on it (downstream).
//
// The graph holds no health. Every node has a Health slot that stays nil
// until the Signal engine (v1.6) fills it.
package graph

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// Presence says whether a node's object exists.
const (
	Present = "present" // the object exists and Opscope has read it
	Missing = "missing" // something refers to it, and it doesn't exist
	Unknown = "unknown" // something refers to it, and Opscope can't read its kind
)

// Edge kinds. The design record's table says what each one is read from.
const (
	Owns       = "owns"       // an owner and what it owns: Deployment → ReplicaSet → Pod
	References = "references" // a pod or pod template, Ingress or Gateway → ConfigMap, Secret, PVC, ServiceAccount
	Binds      = "binds"      // PVC → PV → StorageClass
	RunsOn     = "runs-on"    // Pod → Node
	Selects    = "selects"    // Service → Pod, by label selector
	Endpoints  = "endpoints"  // EndpointSlice → Pod
	Routes     = "routes"     // Ingress, HTTPRoute → Service
	Attaches   = "attaches"   // HTTPRoute → Gateway → GatewayClass
)

// Node is one object, or a placeholder for one that something refers to
// but that doesn't exist (Missing) or can't be read (Unknown).
type Node struct {
	ID              string     `json:"id"`                 // the UID; "missing:<key>" or "unknown:<key>" for placeholders
	Resource        string     `json:"resource,omitempty"` // URL name, e.g. "pods"; empty for a kind Opscope doesn't know
	Kind            string     `json:"kind"`               // e.g. "Pod"
	Namespace       string     `json:"namespace,omitempty"`
	Name            string     `json:"name"`
	Presence        string     `json:"presence"`
	Status          string     `json:"status,omitempty"` // what its list page shows; display only (filled from T3)
	Health          *Health    `json:"health"`           // nil until the Signal engine (v1.6)
	Dangling        []Dangling `json:"dangling,omitempty"`
	ResourceVersion string     `json:"resourceVersion,omitempty"`
}

// Health is filled in by the Signal engine in v1.6. Until then every
// node's Health is nil.
type Health struct {
	State string `json:"state"`
}

// Edge is one typed connection between two nodes. There is one edge per
// source, target and kind; every field that makes it is a piece of evidence.
type Edge struct {
	ID       string     `json:"id"`   // EdgeID(From, To, Kind)
	From     string     `json:"from"` // the dependent
	To       string     `json:"to"`   // what it needs
	Kind     string     `json:"kind"`
	Optional bool       `json:"optional,omitempty"` // every reference behind it is marked optional
	Evidence []Evidence `json:"evidence"`
}

// Evidence is one field that makes an edge (contract 4).
type Evidence struct {
	Builder         string    `json:"builder"`         // the builder that read it, e.g. "pods.volumes"
	Field           string    `json:"field"`           // e.g. "spec.volumes[2].secret.secretName"
	Note            string    `json:"note,omitempty"`  // what the field is, briefly: "env STRIPE_KEY", "ready"
	ResourceVersion string    `json:"resourceVersion"` // of the object the field is in
	Seen            time.Time `json:"seen"`            // when Opscope saw that version
	Optional        bool      `json:"optional,omitempty"`
}

// Dangling is a selector on a node that matches nothing: there's no edge to
// draw, but the gap is worth knowing about.
type Dangling struct {
	Kind   string `json:"kind"`   // the edge kind it would have made, e.g. "selects"
	Field  string `json:"field"`  // e.g. "spec.selector"
	Reason string `json:"reason"` // e.g. "matches no pods"
}

// EdgeID is derived from an edge's content (contract 3): the same two
// nodes and kind always give the same ID, so two graphs from two moments
// can be compared by ID. Each part is length-prefixed, so no two different
// edges run together into the same input.
func EdgeID(from, to, kind string) string {
	h := sha256.New()
	for _, part := range []string{from, to, kind} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}
