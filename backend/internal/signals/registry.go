package signals

import (
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Detector finds signals on objects of some kinds. Detect is pure: the same
// object and context give the same findings, so the engine can run it as
// often as it likes, and a rule pack (v1.13) can add detectors as data.
type Detector struct {
	Name  string   // e.g. "pods.containers"
	Kinds []string // the kinds it's run on, by URL name: "pods"
	Types []string // the signal types it can raise
	// Timed detectors are also run every timedEvery, for conditions that
	// change without the object changing: a certificate nearing expiry,
	// a volume filling up.
	Timed bool
	// Reads are the other kinds it looks up through its Context, so the
	// engine holds them: a claim's StorageClass. A kind it also has a
	// Trigger for needn't be listed.
	Reads []string
	// Triggers re-run the detector on other objects when an object of
	// another kind changes: an EndpointSlice's change re-runs its
	// Service's detectors. Events don't need one: an event always re-runs
	// the detectors of the object it's about.
	Triggers []Trigger
	Detect   func(obj runtime.Object, c Context) []Found
}

// Trigger says which objects to check again when an object of Resource
// changes. Targets sees the world as it is after the change.
type Trigger struct {
	Resource string
	Targets  func(obj runtime.Object, c Context) []Key
}

// Key names an object by kind, namespace and name, for triggers.
type Key struct {
	Resource, Namespace, Name string
}

// Found is one condition a detector found on an object.
type Found struct {
	Type     string
	Severity string // "" for the type's default; never lower than it
	Message  string
	Count    int64 // what the type counts (restarts, events); 0 means 1
	// Since is when the condition started, from the object itself (a
	// condition's lastTransitionTime, an event's first timestamp). Zero
	// means the object doesn't say: it started when Opscope first saw it.
	// A type with a Wait opens once the condition has lasted that long.
	Since    time.Time
	Evidence []Evidence
}

// Context is what a detector may read besides its object: the time, the
// object's events and other objects, all from the same moment.
type Context interface {
	Now() time.Time
	// Events about the object with this UID, newest first.
	Events(uid string) []*corev1.Event
	// Get one object; nil if it doesn't exist or its kind isn't held.
	Get(resource, namespace, name string) runtime.Object
	// List the objects of a kind in a namespace ("" for all, and for
	// cluster-wide kinds), sorted by namespace and name.
	List(resource, namespace string) []runtime.Object
	// Readable says whether the world is given a kind's objects and is up
	// to date with them: when it isn't, a missing object may exist.
	Readable(resource string) bool
	// VolumeFill is how full a claim's volume is, when volume usage is on
	// and the kubelet measured it.
	VolumeFill(namespace, claim string) (Fill, bool)
	// Logs is what reading a failing pod's containers' logs found, by
	// container name: counts only, never a line (see LogFinding).
	Logs(podUID string) map[string]LogFinding
}

// LogFinding is what one read of one container's log found. It holds no
// text from the log (contract 11).
type LogFinding struct {
	Previous bool           // the previous run's log, as for a crash loop
	Read     time.Time      // when it was read
	Lines    int            // lines read
	Patterns map[string]int // pattern name → lines that matched
	Other    int            // kinds of line that matched no pattern
	Error    string         // why it couldn't be read ("not allowed"); the rest is empty
}

// Fill is how full a volume is, as the kubelet measures it.
type Fill struct {
	Used, Capacity     int64 // bytes
	InodesUsed, Inodes int64
	Seen               time.Time
}

// Registry holds the detectors the engine runs (contract 7).
type Registry struct {
	detectors []Detector
}

// Register adds a detector. It panics on a mistake a test would catch: an
// unknown or reserved type, a detector without kinds or a function.
func (r *Registry) Register(d Detector) {
	if d.Name == "" || len(d.Kinds) == 0 || len(d.Types) == 0 || d.Detect == nil {
		panic(fmt.Sprintf("signals: detector %q is incomplete", d.Name))
	}
	for _, t := range d.Types {
		typ, ok := typeOf(t)
		if !ok || typ.Reserved {
			panic(fmt.Sprintf("signals: detector %q raises %q, which isn't in the vocabulary", d.Name, t))
		}
	}
	r.detectors = append(r.detectors, d)
}

// Detectors returns the registered detectors.
func (r *Registry) Detectors() []Detector {
	return slices.Clone(r.detectors)
}

// For returns the detectors run on a kind.
func (r *Registry) For(resource string) []Detector {
	var out []Detector
	for _, d := range r.detectors {
		if slices.Contains(d.Kinds, resource) {
			out = append(out, d)
		}
	}
	return out
}

// Kinds returns every kind the detectors read, with what triggers them,
// and events: what the engine holds.
func (r *Registry) Kinds() []string {
	kinds := []string{"events"}
	for _, d := range r.detectors {
		kinds = append(kinds, d.Kinds...)
		kinds = append(kinds, d.Reads...)
		for _, t := range d.Triggers {
			kinds = append(kinds, t.Resource)
		}
	}
	slices.Sort(kinds)
	return slices.Compact(kinds)
}

// Default holds the built-in detectors.
var Default = &Registry{}
