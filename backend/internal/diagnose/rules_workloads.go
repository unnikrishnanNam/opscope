package diagnose

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"opscope/internal/graph"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

// ---- workload.unavailable: fewer replicas ready than wanted ----

var workloadUnavailable = Rule{ID: "workload.unavailable", Resources: []string{"deployments", "statefulsets", "daemonsets", "replicasets"}, Check: func(c *Context, n *graph.Node) []*Finding {
	if n.State == graph.OK {
		return nil
	}
	var ready, desired int32
	var evidence []Evidence
	ordered := ""
	switch w := c.Object(n).(type) {
	case *appsv1.Deployment:
		if rolloutOf(c, n, w) != nil {
			return nil // deployment.rollout says what's going on
		}
		ready, desired = w.Status.ReadyReplicas, replicaCount(w.Spec.Replicas)
		for _, cond := range w.Status.Conditions {
			if cond.Status != corev1.ConditionTrue || cond.Reason == "ProgressDeadlineExceeded" {
				evidence = append(evidence, conditionEvidence(string(cond.Type), string(cond.Status), cond.Reason, cond.Message, cond.LastTransitionTime))
			}
		}
	case *appsv1.StatefulSet:
		ready, desired = w.Status.ReadyReplicas, replicaCount(w.Spec.Replicas)
		ordered = waitingInOrder(c, n, w)
	case *appsv1.DaemonSet:
		ready, desired = w.Status.NumberReady, w.Status.DesiredNumberScheduled
	case *appsv1.ReplicaSet:
		if !hasPods(c, n.ID) {
			return nil // it can't create them: create-failed says why
		}
		ready, desired = w.Status.ReadyReplicas, replicaCount(w.Spec.Replicas)
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
	case ordered != "":
		f.Detail = ordered
	case f.Severity == Info:
		f.Detail = "It's still getting there: pods are starting or being replaced."
	case len(f.Blames) == 0:
		f.Detail = n.Message
	}
	return []*Finding{f}
}}

// waitingInOrder explains why a StatefulSet has fewer pods than it wants,
// when it starts them one at a time: the last one it started isn't ready,
// so it doesn't start the next.
func waitingInOrder(c *Context, n *graph.Node, ss *appsv1.StatefulSet) string {
	desired := replicaCount(ss.Spec.Replicas)
	if ss.Spec.PodManagementPolicy == appsv1.ParallelPodManagement || ss.Status.Replicas >= desired {
		return ""
	}
	last, lastOrdinal := (*graph.Node)(nil), -1
	for _, owned := range ownedTree(c, n.ID) {
		i := strings.LastIndex(owned.Name, "-")
		if ordinal, err := strconv.Atoi(owned.Name[i+1:]); err == nil && owned.Resource == "pods" && ordinal > lastOrdinal {
			last, lastOrdinal = owned, ordinal
		}
	}
	if last == nil || last.State == graph.OK {
		return ""
	}
	return fmt.Sprintf("It starts its pods one at a time, in order, and waits for each to be ready: %s-%d won't be created until %s is.",
		ss.Name, lastOrdinal+1, last.Name)
}

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

// ---- deployment.rollout: a new revision is rolling out, or stuck ----

const revisionAnnotation = "deployment.kubernetes.io/revision"

// rollout is a Deployment changing from one revision to the next.
type rollout struct {
	next, previous *appsv1.ReplicaSet // previous is nil when there's none left
	nextNode       *graph.Node
	oldServing     int32 // ready pods of every older revision
}

// rolloutOf returns the rollout a Deployment is in, or nil when it isn't
// in one: every replica runs the latest revision and no older pods remain.
// A Deployment that only scales up, or has only ever had one revision,
// isn't rolling out.
func rolloutOf(c *Context, n *graph.Node, d *appsv1.Deployment) *rollout {
	var sets []*appsv1.ReplicaSet
	for _, e := range c.G.Out(n.ID) {
		if rs, ok := c.Object(c.Node(e.To)).(*appsv1.ReplicaSet); ok && e.Type == graph.Owns {
			sets = append(sets, rs)
		}
	}
	if len(sets) < 2 {
		return nil // only one revision: nothing to roll out from
	}
	sort.Slice(sets, func(i, j int) bool { return revision(sets[i]) > revision(sets[j]) })

	r := &rollout{next: sets[0], nextNode: c.Node(key("replicasets", sets[0].Namespace, sets[0].Name))}
	olderPods := false
	for _, rs := range sets[1:] {
		if r.previous == nil {
			r.previous = rs
		}
		if rs.Status.Replicas > 0 {
			olderPods = true
			r.oldServing += rs.Status.ReadyReplicas
		}
	}
	if !olderPods && d.Status.UpdatedReplicas >= replicaCount(d.Spec.Replicas) {
		return nil
	}
	return r
}

func revision(rs *appsv1.ReplicaSet) int {
	n, _ := strconv.Atoi(rs.Annotations[revisionAnnotation])
	return n
}

func key(resource, namespace, name string) string {
	return snapshot.Key{Resource: resource, Namespace: namespace, Name: name}.String()
}

var deploymentRollout = Rule{ID: "deployment.rollout", Resources: []string{"deployments"}, Check: func(c *Context, n *graph.Node) []*Finding {
	d, _ := c.Object(n).(*appsv1.Deployment)
	if d == nil {
		return nil
	}
	r := rolloutOf(c, n, d)
	if r == nil {
		return nil
	}
	rev := r.next.Annotations[revisionAnnotation]
	desired := replicaCount(d.Spec.Replicas)

	f := &Finding{Severity: Info, Confidence: Certain, Next: []Step{eventsOf(n)}}
	var stuck *appsv1.DeploymentCondition
	for i, cond := range d.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing {
			f.Evidence = append(f.Evidence, conditionEvidence(string(cond.Type), string(cond.Status), cond.Reason, cond.Message, cond.LastTransitionTime))
			if cond.Reason == "ProgressDeadlineExceeded" {
				stuck = &d.Status.Conditions[i]
			}
		}
	}
	// What's holding it up: the new revision's pods that aren't OK, or its
	// ReplicaSet when it can't create pods at all.
	if r.nextNode != nil {
		for _, owned := range ownedTree(c, r.nextNode.ID) {
			if owned.State != graph.OK {
				f.Blames = append(f.Blames, owned.ID)
			}
		}
		if r.nextNode.State == graph.Failing && !hasPods(c, r.nextNode.ID) {
			f.Blames = append(f.Blames, r.nextNode.ID)
		}
	}

	switch {
	case stuck != nil:
		f.Title = fmt.Sprintf("Its rollout to revision %s is stuck", rev)
	case len(f.Blames) > 0:
		f.Title = fmt.Sprintf("Revision %s's pods aren't coming up", rev)
	default:
		f.Title = fmt.Sprintf("It's rolling out revision %s", rev)
	}
	if stuck != nil || len(f.Blames) > 0 {
		f.Severity = Degraded
		if r.oldServing == 0 {
			f.Severity = Failing
		}
	}

	if ready := r.next.Status.ReadyReplicas; ready == 0 {
		f.Detail = fmt.Sprintf("No replica runs revision %s yet", rev)
	} else {
		f.Detail = fmt.Sprintf("%d of %d replicas run revision %s", ready, desired, rev)
	}
	if r.oldServing > 0 {
		f.Detail += fmt.Sprintf("; the previous revision still serves with %s", plural(int(r.oldServing), "ready replica", "ready replicas"))
	}
	f.Detail += "."
	if r.previous != nil {
		if changes := templateChanges(r.previous, r.next); len(changes) > 0 {
			f.Detail += fmt.Sprintf(" Revision %s changed %s.", rev, list(changes))
			for _, change := range changes {
				f.Evidence = append(f.Evidence, Evidence{Source: "change", Text: change})
			}
		}
	}
	return []*Finding{f}
}}

// templateChanges says what differs between two revisions' pod templates:
// "the image of container app (busybox:1.36 → busybox:1.37)". It covers what
// a rollout usually changes; anything else is "other settings".
func templateChanges(previous, next *appsv1.ReplicaSet) []string {
	before, after := previous.Spec.Template.Spec, next.Spec.Template.Spec
	var changes []string
	old := map[string]corev1.Container{}
	for _, c := range append(append([]corev1.Container{}, before.InitContainers...), before.Containers...) {
		old[c.Name] = c
	}
	seen := map[string]bool{}
	for _, c := range append(append([]corev1.Container{}, after.InitContainers...), after.Containers...) {
		seen[c.Name] = true
		o, ok := old[c.Name]
		if !ok {
			changes = append(changes, "added container "+c.Name)
			continue
		}
		if o.Image != c.Image {
			changes = append(changes, fmt.Sprintf("the image of container %s (%s → %s)", c.Name, o.Image, c.Image))
		}
		for _, field := range []struct {
			name         string
			before, next any
		}{
			{"command", []any{o.Command, o.Args}, []any{c.Command, c.Args}},
			{"environment", []any{o.Env, o.EnvFrom}, []any{c.Env, c.EnvFrom}},
			{"resources", o.Resources, c.Resources},
			{"probes", []any{o.LivenessProbe, o.ReadinessProbe, o.StartupProbe}, []any{c.LivenessProbe, c.ReadinessProbe, c.StartupProbe}},
			{"ports", o.Ports, c.Ports},
			{"volume mounts", o.VolumeMounts, c.VolumeMounts},
		} {
			if !apiequality.Semantic.DeepEqual(field.before, field.next) {
				changes = append(changes, fmt.Sprintf("the %s of container %s", field.name, c.Name))
			}
		}
	}
	for _, c := range append(append([]corev1.Container{}, before.InitContainers...), before.Containers...) {
		if !seen[c.Name] {
			changes = append(changes, "removed container "+c.Name)
		}
	}
	if !apiequality.Semantic.DeepEqual(before.Volumes, after.Volumes) {
		changes = append(changes, "its volumes")
	}
	if len(changes) == 0 && !apiequality.Semantic.DeepEqual(before, after) {
		changes = append(changes, "other settings of its pods")
	}
	return changes
}

// ---- job.failed: a Job failed, or a CronJob's latest run did ----

var jobFailed = Rule{ID: "job.failed", Resources: []string{"jobs", "cronjobs"}, Check: func(c *Context, n *graph.Node) []*Finding {
	switch obj := c.Object(n).(type) {
	case *batchv1.Job:
		for _, cond := range obj.Status.Conditions {
			if cond.Type != batchv1.JobFailed || cond.Status != corev1.ConditionTrue {
				continue
			}
			f := &Finding{
				Severity:   Failing,
				Confidence: Certain,
				Title:      "It failed",
				Detail:     orElse(cond.Message, cond.Reason),
				Evidence:   []Evidence{conditionEvidence("Failed", "True", cond.Reason, cond.Message, cond.LastTransitionTime)},
				Next:       []Step{eventsOf(n)},
			}
			switch cond.Reason {
			case "BackoffLimitExceeded":
				retries := int32(6)
				if obj.Spec.BackoffLimit != nil {
					retries = *obj.Spec.BackoffLimit
				}
				f.Title = "It failed: every attempt it was allowed failed"
				f.Detail = fmt.Sprintf("It may retry %s after its first attempt (backoffLimit), and none succeeded.", plural(int(retries), "time", "times"))
				if retries == 0 {
					f.Title = "It failed, and may not retry"
					f.Detail = "Its only attempt failed, and its backoffLimit of 0 allows no more."
				}
			case "DeadlineExceeded":
				f.Title = "It ran out of time"
				if obj.Spec.ActiveDeadlineSeconds != nil {
					f.Detail = fmt.Sprintf("It may run for %ds (activeDeadlineSeconds), and it hadn't finished.", *obj.Spec.ActiveDeadlineSeconds)
				}
			}
			for _, owned := range ownedTree(c, n.ID) {
				if owned.Resource == "pods" && owned.State != graph.OK {
					f.Blames = append(f.Blames, owned.ID)
				}
			}
			return []*Finding{f}
		}
	case *batchv1.CronJob:
		if obj.Spec.Suspend != nil && *obj.Spec.Suspend {
			return []*Finding{{Severity: Info, Confidence: Certain, Title: "It's suspended",
				Detail: "It won't start new runs until it's resumed (spec.suspend)."}}
		}
		if n.State != graph.Degraded {
			return nil
		}
		var latest *batchv1.Job
		for _, owned := range ownedTree(c, n.ID) {
			if job, ok := c.Object(owned).(*batchv1.Job); ok && (latest == nil || job.CreationTimestamp.After(latest.CreationTimestamp.Time)) {
				latest = job
			}
		}
		if latest == nil {
			return nil
		}
		return []*Finding{{
			Severity:   Degraded,
			Confidence: Certain,
			Title:      "Its latest run failed",
			Detail: fmt.Sprintf("Job %s, started %s ago, failed. It runs on the schedule %q, so the next run may go the same way.",
				latest.Name, ago(c.Now.Sub(latest.CreationTimestamp.Time)), obj.Spec.Schedule),
			Blames: []string{key("jobs", latest.Namespace, latest.Name)},
			Next:   []Step{eventsOf(n)},
		}}
	}
	return nil
}}

// ---- hpa.scaling: an autoscaler can't scale ----

var hpaScaling = Rule{ID: "hpa.scaling", Resources: []string{"horizontalpodautoscalers"}, Check: func(c *Context, n *graph.Node) []*Finding {
	hpa, _ := c.Object(n).(*autoscalingv2.HorizontalPodAutoscaler)
	if hpa == nil {
		return nil
	}
	var findings []*Finding
	for _, e := range c.G.Out(n.ID) {
		if target := c.Node(e.To); e.Type == graph.Scales && target.State == graph.Missing {
			findings = append(findings, &Finding{Severity: Failing, Confidence: Certain,
				Title:  fmt.Sprintf("What it scales, %s, doesn't exist", describe(target, n.Namespace)),
				Blames: []string{target.ID}})
		}
	}
	for _, cond := range hpa.Status.Conditions {
		evidence := []Evidence{conditionEvidence(string(cond.Type), string(cond.Status), cond.Reason, cond.Message, cond.LastTransitionTime)}
		switch {
		case cond.Type == autoscalingv2.ScalingActive && cond.Status == corev1.ConditionFalse:
			f := &Finding{Severity: Degraded, Confidence: Certain, Title: "It can't scale: " + lowerFirst(cond.Message), Evidence: evidence,
				Next: []Step{kubectl("describe", n)}}
			if strings.HasPrefix(cond.Reason, "FailedGet") {
				f.Title = "It can't read the metrics it scales by"
				f.Detail = upperFirst(cond.Message) + "."
				if strings.Contains(cond.Message, "metrics.k8s.io") || strings.Contains(cond.Message, "resource metrics API") {
					f.Detail += " CPU and memory metrics come from metrics-server: is it installed and running?"
				}
			}
			findings = append(findings, f)
		case cond.Type == autoscalingv2.AbleToScale && cond.Status == corev1.ConditionFalse:
			findings = append(findings, &Finding{Severity: Degraded, Confidence: Certain, Title: "It can't change its target's size",
				Detail: cond.Message, Evidence: evidence})
		case cond.Type == autoscalingv2.ScalingLimited && cond.Status == corev1.ConditionTrue && cond.Reason == "TooManyReplicas":
			findings = append(findings, &Finding{Severity: Info, Confidence: Certain,
				Title:  fmt.Sprintf("It's at its maximum of %d replicas", hpa.Spec.MaxReplicas),
				Detail: "It would scale further if it were allowed to. If the load stays this high, raise maxReplicas.", Evidence: evidence})
		}
	}
	return findings
}}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
