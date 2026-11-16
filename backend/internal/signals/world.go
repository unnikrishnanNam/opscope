package signals

import (
	"cmp"
	"slices"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

const (
	// resolveAfter is how long a condition must stay gone before its
	// signal resolves: a probe that fails one check in three doesn't open
	// and close a signal every few seconds.
	resolveAfter = 60 * time.Second
	// keepResolved is how long resolved signals are kept, for the Issues
	// page's "Resolved" tab. Until the State store (v1.7) they're in memory.
	keepResolved = time.Hour
	// timedEvery is how often timed detectors run over all their objects.
	timedEvery = 60 * time.Second
	// maxTransitions caps the health history per cluster; the oldest go first.
	maxTransitions = 10_000
)

// world is one cluster's objects, as the detectors read them, and its
// signals and health. It isn't safe for concurrent use: the engine holds
// a lock around every call. Every change takes the time it happens at, so
// tests decide what time it is.
type world struct {
	cluster string
	reg     *Registry
	holds   map[string]bool
	// onRequest worlds are built for one answer, with no history: a
	// condition whose start the object doesn't say is taken as having
	// lasted long enough.
	onRequest bool

	objects    map[string]map[string]runtime.Object // resource → UID → object
	keys       map[string]string                    // objectKey → UID
	resourceOf map[string]string                    // UID → resource
	events     map[string]map[string]*corev1.Event  // involved UID → event UID → event
	eventAbout map[string]string                    // event UID → involved UID

	signals   map[string]*Signal         // by ID: open, resolving, and resolved for keepResolved
	byUID     map[string]map[string]bool // UID → IDs of its open and resolving signals
	resolving map[string]time.Time       // signal ID → when its condition went away
	pending   map[string]pendingSignal   // signal ID → a condition waiting out its type's Wait

	health    map[string]Health // UIDs whose health has changed since Opscope started, or isn't healthy
	published *sync.Map         // UID → *Health, for readers that can't take the engine's lock (the graph)
	history   []Transition

	canRead   map[string]bool                  // kinds the world is given objects of
	stale     map[string]bool                  // kinds behind a collector gap: their signals aren't checked
	fills     map[string]Fill                  // "namespace/claim" → volume usage, when it's on (setFills)
	logs      map[string]map[string]LogFinding // pod UID → container → what its log said (setLog)
	lastTimed time.Time
	started   time.Time // when this world was first given objects: how far back history goes

	out []bus.Event // to publish once the engine lets go of its lock
}

type pendingSignal struct {
	uid, typ string
	since    time.Time
}

func newWorld(cluster string, reg *Registry, now time.Time) *world {
	holds := map[string]bool{}
	for _, k := range reg.Kinds() {
		holds[k] = true
	}
	return &world{
		cluster: cluster, reg: reg, holds: holds,
		objects: map[string]map[string]runtime.Object{}, keys: map[string]string{}, resourceOf: map[string]string{},
		events: map[string]map[string]*corev1.Event{}, eventAbout: map[string]string{},
		signals: map[string]*Signal{}, byUID: map[string]map[string]bool{},
		resolving: map[string]time.Time{}, pending: map[string]pendingSignal{},
		health: map[string]Health{}, published: &sync.Map{},
		logs: map[string]map[string]LogFinding{}, canRead: map[string]bool{}, stale: map[string]bool{}, lastTimed: now, started: now,
	}
}

func objectKey(resource, namespace, name string) string {
	return resource + "\x00" + namespace + "\x00" + name
}

// upsert adds or changes an object, and checks again everything it bears on.
func (w *world) upsert(resource string, obj runtime.Object, now time.Time) {
	old, ok := w.store(resource, obj)
	if !ok {
		return
	}
	w.check(w.bearsOn(resource, old, obj), now)
}

// remove deletes an object. Its signals resolve at once: the object is
// gone, so the condition can't come back on it.
func (w *world) remove(resource, uid string, now time.Time) {
	if obj := w.forget(resource, uid, now); obj != nil {
		w.check(w.bearsOn(resource, obj, nil), now)
	}
}

// replace sets every object of a kind, after a first list or a gap. Every
// object is in place before anything is checked: a Deployment naming two
// Secrets checked when only the first had arrived would find the second
// missing.
func (w *world) replace(resource string, objects []runtime.Object, now time.Time) {
	if !w.holds[resource] {
		return
	}
	delete(w.stale, resource)
	keep := map[string]bool{}
	for _, obj := range objects {
		if m, err := meta.Accessor(obj); err == nil {
			keep[string(m.GetUID())] = true
		}
	}
	type change struct{ old, new runtime.Object }
	var changes []change
	for _, uid := range sortedKeys(w.objects[resource]) {
		if !keep[uid] {
			if obj := w.forget(resource, uid, now); obj != nil {
				changes = append(changes, change{obj, nil})
			}
		}
	}
	for _, obj := range objects {
		if old, ok := w.store(resource, obj); ok {
			changes = append(changes, change{old, obj})
		}
	}
	due := map[string]bool{}
	for _, c := range changes {
		for uid := range w.bearsOn(resource, c.old, c.new) {
			due[uid] = true
		}
	}
	w.check(due, now)
}

// store puts an object in place without checking anything; ok is false
// for a kind the world doesn't hold. old is what it replaced, or nil.
func (w *world) store(resource string, obj runtime.Object) (old runtime.Object, ok bool) {
	if !w.holds[resource] {
		return nil, false
	}
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil, false
	}
	uid := string(m.GetUID())
	if w.objects[resource] == nil {
		w.objects[resource] = map[string]runtime.Object{}
	}
	old = w.objects[resource][uid]
	w.objects[resource][uid] = obj
	w.keys[objectKey(resource, m.GetNamespace(), m.GetName())] = uid
	w.resourceOf[uid] = resource
	if e, isEvent := obj.(*corev1.Event); isEvent {
		w.indexEvent(e)
	}
	return old, true
}

// forget takes an object out, and resolves its signals; nil if it wasn't
// there. What it bears on isn't checked.
func (w *world) forget(resource, uid string, now time.Time) runtime.Object {
	obj, ok := w.objects[resource][uid]
	if !ok {
		return nil
	}
	delete(w.objects[resource], uid)
	delete(w.resourceOf, uid)
	if m, err := meta.Accessor(obj); err == nil {
		delete(w.keys, objectKey(resource, m.GetNamespace(), m.GetName()))
	}
	if _, isEvent := obj.(*corev1.Event); isEvent {
		about := w.eventAbout[uid]
		delete(w.eventAbout, uid)
		delete(w.events[about], uid)
		if len(w.events[about]) == 0 {
			delete(w.events, about)
		}
		return obj
	}
	for id := range w.byUID[uid] {
		w.resolve(id, now)
	}
	for id, p := range w.pending {
		if p.uid == uid {
			delete(w.pending, id)
		}
	}
	delete(w.health, uid)
	delete(w.logs, uid)
	w.published.Delete(uid)
	return obj
}

// bearsOn is the UIDs to check after an object changed from old to new
// (either nil): the object itself, the object an event is about, and what
// the detectors' triggers name.
func (w *world) bearsOn(resource string, old, new runtime.Object) map[string]bool {
	due := map[string]bool{}
	for _, obj := range []runtime.Object{old, new} {
		if obj == nil {
			continue
		}
		if e, isEvent := obj.(*corev1.Event); isEvent {
			due[string(e.InvolvedObject.UID)] = true
			continue
		}
		if m, err := meta.Accessor(obj); err == nil {
			due[string(m.GetUID())] = true
		}
		for _, d := range w.reg.detectors {
			for _, t := range d.Triggers {
				if t.Resource != resource {
					continue
				}
				for _, k := range t.Targets(obj, worldView{w: w}) {
					if uid, ok := w.keys[objectKey(k.Resource, k.Namespace, k.Name)]; ok {
						due[uid] = true
					}
				}
			}
		}
	}
	return due
}

// check evaluates objects in a fixed order. One that's gone is skipped.
func (w *world) check(due map[string]bool, now time.Time) {
	for _, uid := range sortedKeys(due) {
		w.evaluate(uid, now)
	}
}

func (w *world) readable(resource string) bool {
	return w.canRead[resource]
}

// setReadable says whether the world is given a kind's objects. A kind
// that can no longer be read (a permission taken away) keeps its objects
// and signals, marked stale: a signal never resolves because data is
// missing.
func (w *world) setReadable(resource string, readable bool) {
	if w.canRead[resource] && !readable {
		w.markStale(resource)
	}
	w.canRead[resource] = readable
}

// markStale marks a kind as behind a gap: its objects' signals stay as
// they are, flagged stale, until replace reads the kind again.
func (w *world) markStale(resource string) {
	if !w.holds[resource] {
		return
	}
	w.stale[resource] = true
	for uid := range w.objects[resource] {
		for id := range w.byUID[uid] {
			w.signals[id].Stale = true
		}
	}
}

func (w *world) indexEvent(e *corev1.Event) {
	uid, about := string(e.UID), string(e.InvolvedObject.UID)
	if prev, ok := w.eventAbout[uid]; ok && prev != about {
		delete(w.events[prev], uid)
	}
	w.eventAbout[uid] = about
	if w.events[about] == nil {
		w.events[about] = map[string]*corev1.Event{}
	}
	w.events[about][uid] = e
}

// evaluate runs an object's detectors and brings its signals in line with
// what they find.
func (w *world) evaluate(uid string, now time.Time) {
	resource, ok := w.resourceOf[uid]
	if !ok || resource == "events" || w.stale[resource] {
		return
	}
	obj := w.objects[resource][uid]
	detectors := w.reg.For(resource)
	if len(detectors) == 0 {
		return
	}

	view := worldView{w, now}
	found := map[string]Found{}
	var order []string
	for _, d := range detectors {
		for _, f := range d.Detect(obj, view) {
			for i := range f.Evidence {
				if f.Evidence[i].Detector == "" {
					f.Evidence[i].Detector = d.Name
				}
				if f.Evidence[i].Seen.IsZero() {
					f.Evidence[i].Seen = now
				}
			}
			if prev, ok := found[f.Type]; ok {
				// Two detectors found the same thing: one signal, with both
				// detectors' evidence and the worse severity.
				prev.Evidence = append(prev.Evidence, f.Evidence...)
				if severityRank(f.Severity) > severityRank(prev.Severity) {
					prev.Severity = f.Severity
				}
				found[f.Type] = prev
				continue
			}
			found[f.Type] = f
			order = append(order, f.Type)
		}
	}

	for _, typ := range order {
		w.apply(uid, resource, obj, found[typ], now)
	}
	for id := range w.byUID[uid] {
		s := w.signals[id]
		if _, still := found[s.Type]; !still && s.State == Open {
			s.State = Resolving
			s.Stale = false
			w.resolving[id] = now
		}
	}
	for id, p := range w.pending {
		if _, still := found[p.typ]; p.uid == uid && !still {
			delete(w.pending, id) // it went away before its wait was over
		}
	}
	w.updateHealth(uid, now)
}

// apply opens or updates one signal from what a detector found.
func (w *world) apply(uid, resource string, obj runtime.Object, f Found, now time.Time) {
	t, _ := typeOf(f.Type)
	id := SignalID(w.cluster, uid, f.Type)
	severity := t.Severity
	if severityRank(f.Severity) > severityRank(severity) {
		severity = f.Severity
	}
	count := max(f.Count, 1)
	evidence := f.Evidence
	if evidence == nil {
		evidence = []Evidence{}
	}

	if s := w.signals[id]; s != nil && s.State != Resolved {
		s.State, s.Stale = Open, false
		s.Severity, s.Message, s.Count, s.Evidence, s.LastSeen = severity, f.Message, count, evidence, now
		delete(w.resolving, id)
		return
	}

	since := f.Since
	if since.IsZero() {
		if p, ok := w.pending[id]; ok {
			since = p.since
		} else if !w.onRequest {
			since = now
		}
	}
	if !since.IsZero() && now.Sub(since) < t.Wait {
		w.pending[id] = pendingSignal{uid: uid, typ: f.Type, since: since}
		return
	}
	delete(w.pending, id)
	if since.IsZero() {
		since = now
	}

	s := &Signal{
		ID: id, Type: f.Type, Severity: severity, Cluster: w.cluster,
		Resource: refOf(resource, obj), Message: f.Message,
		FirstSeen: since, LastSeen: now, Count: count, State: Open, Evidence: evidence,
	}
	w.signals[id] = s
	if w.byUID[uid] == nil {
		w.byUID[uid] = map[string]bool{}
	}
	w.byUID[uid][id] = true
	w.out = append(w.out, bus.SignalRaised{Cluster: w.cluster, ID: id, UID: uid, Type: s.Type, Severity: severity, At: now})
}

// resolve closes a signal.
func (w *world) resolve(id string, now time.Time) {
	s := w.signals[id]
	if s == nil || s.State == Resolved {
		return
	}
	uid := s.Resource.UID
	s.State, s.Stale = Resolved, false
	at := now
	s.Resolved = &at
	delete(w.resolving, id)
	delete(w.byUID[uid], id)
	if len(w.byUID[uid]) == 0 {
		delete(w.byUID, uid)
	}
	w.out = append(w.out, bus.SignalResolved{Cluster: w.cluster, ID: id, UID: uid, Type: s.Type, At: now})
	if _, exists := w.resourceOf[uid]; exists {
		w.updateHealth(uid, now)
	}
}

// tick does what time alone does: resolves signals whose condition stayed
// away, opens those that waited long enough, runs the timed detectors and
// forgets old resolved signals.
func (w *world) tick(now time.Time) {
	for _, id := range sortedKeys(w.resolving) {
		if now.Sub(w.resolving[id]) >= resolveAfter {
			w.resolve(id, now)
		}
	}
	due := map[string]bool{}
	for _, p := range w.pending {
		if t, _ := typeOf(p.typ); now.Sub(p.since) >= t.Wait {
			due[p.uid] = true
		}
	}
	for _, uid := range sortedKeys(due) {
		w.evaluate(uid, now)
	}
	if now.Sub(w.lastTimed) >= timedEvery {
		w.lastTimed = now
		for _, d := range w.reg.detectors {
			if !d.Timed {
				continue
			}
			for _, resource := range d.Kinds {
				for _, uid := range sortedKeys(w.objects[resource]) {
					w.evaluate(uid, now)
				}
			}
		}
	}
	for id, s := range w.signals {
		if s.State == Resolved && now.Sub(*s.Resolved) > keepResolved {
			delete(w.signals, id)
		}
	}
}

// updateHealth works out an object's health from its open signals and
// records a change. Objects of a kind no detector reads have no health.
func (w *world) updateHealth(uid string, now time.Time) {
	resource := w.resourceOf[uid]
	if len(w.reg.For(resource)) == 0 {
		return
	}
	state, worst := Healthy, 0
	var ids []string
	for id := range w.byUID[uid] {
		ids = append(ids, id)
		if r := severityRank(w.signals[id].Severity); r > worst {
			worst = r
		}
	}
	slices.Sort(ids)
	switch {
	case worst >= severityRank(Critical):
		state = Failing
	case worst >= severityRank(Warning):
		state = Degraded
	}

	prev, known := w.health[uid]
	if !known {
		prev = Health{State: Healthy}
		if state == Healthy {
			return // healthy, as it's been since Opscope first saw it
		}
	}
	next := Health{State: state, Since: prev.Since, Signals: ids}
	if prev.State != state {
		next.Since = now
		w.record(Transition{UID: uid, From: prev.State, To: state, At: now, Signals: ids})
		w.out = append(w.out, bus.HealthChanged{Cluster: w.cluster, UID: uid, From: prev.State, To: state, At: now})
	}
	w.health[uid] = next
	h := next
	w.published.Store(uid, &h)
}

func (w *world) record(t Transition) {
	w.history = append(w.history, t)
	if len(w.history) >= maxTransitions+maxTransitions/10 {
		w.history = slices.Clone(w.history[len(w.history)-maxTransitions:])
	}
}

// setFills replaces the volume usage the detectors read, and checks the
// claims again. nil means volume usage is off or couldn't be read: claims
// keep the signals they have, since a signal never resolves for want of
// data, and their fill isn't checked until it can be read again.
func (w *world) setFills(fills map[string]Fill, now time.Time) {
	if fills == nil {
		return
	}
	w.fills = fills
	for _, uid := range sortedKeys(w.objects["persistentvolumeclaims"]) {
		w.evaluate(uid, now)
	}
}

// setLog records what a container's log said and checks its pod again.
// A pod that's gone is ignored.
func (w *world) setLog(podUID, container string, f LogFinding, now time.Time) {
	if _, ok := w.objects["pods"][podUID]; !ok {
		return
	}
	if w.logs[podUID] == nil {
		w.logs[podUID] = map[string]LogFinding{}
	}
	w.logs[podUID][container] = f
	w.evaluate(podUID, now)
}

// LogWant is a container whose log is worth reading: its pod has an open
// crash_loop (the previous run's log) or probe_failing (the current one).
type LogWant struct {
	PodUID, Namespace, Pod, Container string
	Previous                          bool
}

// logWants lists the containers whose logs are worth reading now, in a
// fixed order. Which are read, and how often, is the engine's to decide.
func (w *world) logWants() []LogWant {
	var out []LogWant
	for _, uid := range sortedKeys(w.byUID) {
		pod, ok := w.objects["pods"][uid].(*corev1.Pod)
		if !ok {
			continue
		}
		// A crash loop resolves for a moment each time its container runs
		// again, so resolving counts; its signal comes back with the next
		// exit, and the finding with it.
		var crash, probe bool
		for id := range w.byUID[uid] {
			s := w.signals[id]
			crash = crash || s.Type == "crash_loop"
			probe = probe || s.Type == "probe_failing" && s.State == Open
		}
		for _, cs := range pod.Status.ContainerStatuses {
			looping := cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" ||
				cs.LastTerminationState.Terminated != nil && cs.RestartCount > 0
			switch {
			case crash && looping:
				out = append(out, LogWant{uid, pod.Namespace, pod.Name, cs.Name, true})
			case probe && cs.State.Running != nil && !cs.Ready:
				out = append(out, LogWant{uid, pod.Namespace, pod.Name, cs.Name, false})
			}
		}
	}
	return out
}

// drain hands over the bus events made since the last drain.
func (w *world) drain() []bus.Event {
	out := w.out
	w.out = nil
	return out
}

// ---- Reading ----

// worldView is what detectors read (Context): the world at one moment.
type worldView struct {
	w   *world
	now time.Time
}

func (v worldView) Now() time.Time { return v.now }

func (v worldView) Events(uid string) []*corev1.Event {
	var out []*corev1.Event
	for _, e := range v.w.events[uid] {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := eventTime(out[i]), eventTime(out[j])
		if !a.Equal(b) {
			return a.After(b)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (v worldView) Readable(resource string) bool {
	return v.w.readable(resource) && !v.w.stale[resource]
}

func (v worldView) Logs(podUID string) map[string]LogFinding {
	return v.w.logs[podUID]
}

func (v worldView) VolumeFill(namespace, claim string) (Fill, bool) {
	f, ok := v.w.fills[namespace+"/"+claim]
	return f, ok
}

func (v worldView) Get(resource, namespace, name string) runtime.Object {
	uid, ok := v.w.keys[objectKey(resource, namespace, name)]
	if !ok {
		return nil
	}
	return v.w.objects[resource][uid]
}

func (v worldView) List(resource, namespace string) []runtime.Object {
	var out []runtime.Object
	for _, obj := range v.w.objects[resource] {
		if m, err := meta.Accessor(obj); err == nil && (namespace == "" || m.GetNamespace() == namespace) {
			out = append(out, obj)
		}
	}
	slices.SortFunc(out, func(a, b runtime.Object) int {
		ma, _ := meta.Accessor(a)
		mb, _ := meta.Accessor(b)
		return cmp.Or(cmp.Compare(ma.GetNamespace(), mb.GetNamespace()), cmp.Compare(ma.GetName(), mb.GetName()))
	})
	return out
}

// eventTime is when an event last happened, from whichever field says so.
func eventTime(e *corev1.Event) time.Time {
	switch {
	case e.Series != nil && !e.Series.LastObservedTime.IsZero():
		return e.Series.LastObservedTime.Time
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

// refOf names an object.
func refOf(resource string, obj runtime.Object) Ref {
	r := Ref{Resource: resource}
	if k, ok := resources.Lookup(resource); ok {
		r.Kind = k.KindName
	}
	if m, err := meta.Accessor(obj); err == nil {
		r.UID, r.Namespace, r.Name = string(m.GetUID()), m.GetNamespace(), m.GetName()
	}
	return r
}

// ownerOf is the workload that controls an object, for grouping on the
// Issues page: a pod's ReplicaSet's Deployment when there is one. It's
// worked out when signals are read, so it never depends on the order
// objects arrived in.
func (w *world) ownerOf(resource, uid string) *Ref {
	obj := w.objects[resource][uid]
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil
	}
	c := metav1.GetControllerOfNoCopy(m)
	if c == nil {
		return nil
	}
	owner := ownerRef(c, m.GetNamespace())
	if owner.Resource == "replicasets" {
		if rs := (worldView{w: w}).Get("replicasets", owner.Namespace, owner.Name); rs != nil {
			if rm, err := meta.Accessor(rs); err == nil {
				if up := metav1.GetControllerOfNoCopy(rm); up != nil {
					o := ownerRef(up, owner.Namespace)
					return &o
				}
			}
		}
	}
	return &owner
}

func ownerRef(o *metav1.OwnerReference, namespace string) Ref {
	r := Ref{UID: string(o.UID), Kind: o.Kind, Namespace: namespace, Name: o.Name}
	group := ""
	if gv, err := schemaGroup(o.APIVersion); err == nil {
		group = gv
	}
	if k, ok := resources.LookupKind(group, o.Kind); ok {
		r.Resource = k.Resource
	}
	return r
}

// schemaGroup is the group of an apiVersion: "apps" for "apps/v1", "" for "v1".
func schemaGroup(apiVersion string) (string, error) {
	for i := range len(apiVersion) {
		if apiVersion[i] == '/' {
			return apiVersion[:i], nil
		}
	}
	return "", nil
}

// signal returns a copy of a signal, with its owner filled in.
func (w *world) signal(id string) (Signal, bool) {
	s, ok := w.signals[id]
	if !ok {
		return Signal{}, false
	}
	c := *s
	c.Evidence = slices.Clone(s.Evidence)
	if _, exists := w.resourceOf[s.Resource.UID]; exists {
		c.Owner = w.ownerOf(s.Resource.Resource, s.Resource.UID)
	}
	return c, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
