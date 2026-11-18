package signals

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/signals/logs"
)

// Detectors on pods: their containers' states, their conditions, and the
// events about them. A pod being deleted has no signals: whatever it was
// doing, it's going away.

// eventWindow is how recent an event must be to count as the condition
// still holding: Kubernetes keeps events for an hour, long after most
// problems are over. Detectors that read events are timed, so a signal
// resolves once its events grow old even if nothing else changes.
const eventWindow = 10 * time.Minute

// oomWindow is how long a container's out-of-memory kill stays a signal,
// so a pod that was killed once and runs again is still seen for a while.
const oomWindow = time.Hour

func init() {
	Default.Register(Detector{
		Name:   "pods.containers",
		Kinds:  []string{"pods"},
		Types:  []string{"crash_loop", "image_pull_failed", "config_missing", "init_failing", "container_error"},
		Detect: podContainers,
	})
	Default.Register(Detector{
		Name:   "pods.oom",
		Kinds:  []string{"pods"},
		Types:  []string{"oom_killed"},
		Timed:  true, // it ends an hour after the kill, with nothing changing
		Detect: podOOM,
	})
	Default.Register(Detector{
		Name:   "pods.status",
		Kinds:  []string{"pods"},
		Types:  []string{"unschedulable", "evicted"},
		Detect: podStatus,
	})
	Default.Register(Detector{
		Name:   "pods.events",
		Kinds:  []string{"pods"},
		Types:  []string{"probe_failing", "mount_failed", "unschedulable"},
		Timed:  true, // events grow old
		Detect: podEvents,
	})
}

// containerState is one container's status with where it sits in the pod.
type containerState struct {
	status corev1.ContainerStatus
	field  string // "status.containerStatuses[0]"
	init   bool   // an init container that must finish (not a sidecar)
}

func containersOf(pod *corev1.Pod) []containerState {
	var out []containerState
	for i, s := range pod.Status.InitContainerStatuses {
		out = append(out, containerState{s, fmt.Sprintf("status.initContainerStatuses[%d]", i), !isSidecar(pod, s.Name)})
	}
	for i, s := range pod.Status.ContainerStatuses {
		out = append(out, containerState{s, fmt.Sprintf("status.containerStatuses[%d]", i), false})
	}
	return out
}

func isSidecar(pod *corev1.Pod, name string) bool {
	for _, c := range pod.Spec.InitContainers {
		if c.Name == name {
			return c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways
		}
	}
	return false
}

// found gathers one type's finding over several containers.
type found struct {
	Found
	names []string
}

func (f *found) add(name string, ev Evidence) {
	f.names = append(f.names, name)
	f.Evidence = append(f.Evidence, ev)
}

// who: "Container app", "Containers app, sidecar", "Init container setup".
func who(names []string, init bool) string {
	what := "Container"
	if init {
		what = "Init container"
	}
	if len(names) > 1 {
		return what + "s " + strings.Join(names, ", ")
	}
	return what + " " + names[0]
}

// podContainers reads each container's state: crash loops, images that
// can't be pulled, config that's missing, init containers that fail, and
// containers that exited with an error and won't run again.
func podContainers(obj runtime.Object, c Context) []Found {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.DeletionTimestamp != nil {
		return nil
	}
	rv := pod.ResourceVersion
	field := func(path, value string) Evidence {
		return Evidence{Source: FromField, Field: path, Value: value, ResourceVersion: rv}
	}
	var crash, pull, config, initFail, exitErr found
	var pullMessage, configMessage string
	for _, cs := range containersOf(pod) {
		s := cs.status
		if w := s.State.Waiting; w != nil {
			path := cs.field + ".state.waiting.reason"
			switch w.Reason {
			case "CrashLoopBackOff":
				if cs.init {
					initFail.add(s.Name, field(path, w.Reason))
					continue
				}
				crash.add(s.Name, field(path, w.Reason))
				crash.Count += int64(s.RestartCount)
				if t := s.LastTerminationState.Terminated; t != nil {
					crash.Evidence = append(crash.Evidence, field(cs.field+".lastState.terminated", exitText(t)))
				}
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrImageNeverPull":
				pull.add(s.Name, field(path, w.Reason))
				if pullMessage == "" {
					pullMessage = fmt.Sprintf("Image %s can't be pulled (%s)", imageOf(pod, s.Name), w.Reason)
				}
			case "CreateContainerConfigError", "CreateContainerError":
				config.add(s.Name, field(path, w.Reason))
				if configMessage == "" {
					configMessage = w.Message
				}
			}
			continue
		}
		if t := s.State.Terminated; t != nil && !cs.init && restartsSoon(pod, s, t) {
			// Between a crash and its back-off, the kubelet shows the
			// container as terminated, for more than a minute once the
			// back-off grows: still a crash loop, or its signal would
			// resolve and open again with every restart.
			crash.add(s.Name, field(cs.field+".state.terminated", exitText(t)))
			crash.Count += int64(s.RestartCount)
			continue
		}
		if t := s.State.Terminated; t != nil && t.ExitCode != 0 {
			path := cs.field + ".state.terminated"
			switch {
			case cs.init:
				initFail.add(s.Name, field(path, exitText(t)))
			case pod.Spec.RestartPolicy == corev1.RestartPolicyNever && !ownedBy(pod, "Job"):
				// A Job's failed pods are its Job's to judge (job_failed):
				// it retries, and keeps the failures it's done with.
				exitErr.add(s.Name, field(path, exitText(t)))
			}
		}
	}

	var out []Found
	if crash.names != nil {
		crash.Type = "crash_loop"
		crash.Message = crashMessage(pod, crash.names, crash.Count)
		addLogs(&crash.Found, c.Logs(string(pod.UID)), crash.names, true)
		out = append(out, crash.Found)
	}
	if pull.names != nil {
		pull.Type = "image_pull_failed"
		pull.Message = pullMessage
		out = append(out, pull.Found)
	}
	if config.names != nil {
		config.Type = "config_missing"
		config.Message = who(config.names, false) + " can't start"
		if configMessage != "" {
			config.Message += ": " + configMessage
		}
		out = append(out, config.Found)
	}
	if initFail.names != nil {
		initFail.Type = "init_failing"
		initFail.Message = who(initFail.names, true) + " fails, so the pod doesn't start"
		out = append(out, initFail.Found)
	}
	if exitErr.names != nil {
		exitErr.Type = "container_error"
		exitErr.Message = who(exitErr.names, false) + " exited with an error and won't be restarted"
		out = append(out, exitErr.Found)
	}
	return out
}

// podOOM finds containers killed for using too much memory within the last
// hour, now or in their last run.
func podOOM(obj runtime.Object, c Context) []Found {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.DeletionTimestamp != nil {
		return nil
	}
	var f found
	var last time.Time
	for _, cs := range containersOf(pod) {
		for _, st := range []struct {
			path string
			t    *corev1.ContainerStateTerminated
		}{
			{cs.field + ".state.terminated", cs.status.State.Terminated},
			{cs.field + ".lastState.terminated", cs.status.LastTerminationState.Terminated},
		} {
			path, t := st.path, st.t
			if t == nil || t.Reason != "OOMKilled" || c.Now().Sub(t.FinishedAt.Time) > oomWindow {
				continue
			}
			f.add(cs.status.Name, Evidence{Source: FromField, Field: path, Value: exitText(t), ResourceVersion: pod.ResourceVersion})
			if t.FinishedAt.After(last) {
				last = t.FinishedAt.Time
			}
		}
	}
	if f.names == nil {
		return nil
	}
	f.names = unique(f.names)
	f.Type = "oom_killed"
	f.Since = last
	f.Message = fmt.Sprintf("%s ran out of memory%s; last killed at %s", who(f.names, false),
		limitText(pod, f.names[0]), last.UTC().Format("15:04 UTC"))
	return []Found{f.Found}
}

// podStatus reads the pod's conditions and reason: whether it can be
// scheduled, and whether it was evicted.
func podStatus(obj runtime.Object, c Context) []Found {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.DeletionTimestamp != nil {
		return nil
	}
	var out []Found
	for i, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == corev1.PodReasonUnschedulable {
			out = append(out, Found{
				Type: "unschedulable", Since: cond.LastTransitionTime.Time,
				Message: "No node can run it: " + firstLine(orElse(cond.Message, "the scheduler gave no reason")),
				Evidence: []Evidence{{Source: FromField, Field: fmt.Sprintf("status.conditions[%d]", i),
					Value: "PodScheduled=False (Unschedulable)", ResourceVersion: pod.ResourceVersion}},
			})
		}
	}
	if pod.Status.Reason == "Evicted" {
		out = append(out, Found{
			Type: "evicted", Message: "Evicted: " + orElse(pod.Status.Message, "no reason given"),
			Evidence: []Evidence{{Source: FromField, Field: "status.reason", Value: "Evicted", ResourceVersion: pod.ResourceVersion}},
		})
	}
	return out
}

// podEvents reads recent events about a pod: failing probes on a running
// pod that isn't ready, volumes that can't be mounted, and the scheduler
// giving up (evidence for unschedulable, with the condition).
func podEvents(obj runtime.Object, c Context) []Found {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.DeletionTimestamp != nil {
		return nil
	}
	var probe, mount, sched Found
	for _, e := range c.Events(string(pod.UID)) {
		if e.Type != corev1.EventTypeWarning || c.Now().Sub(eventTime(e)) > eventWindow {
			continue
		}
		switch {
		case e.Reason == "Unhealthy" && (strings.Contains(e.Message, "Readiness probe") || strings.Contains(e.Message, "Liveness probe") ||
			strings.Contains(e.Message, "Startup probe")):
			if !runningNotReady(pod) {
				continue
			}
			addEvent(&probe, e)
			if probe.Message == "" {
				probe.Message = firstLine(e.Message)
			}
		case e.Reason == "FailedMount" || e.Reason == "FailedAttachVolume":
			if pod.Status.Phase != corev1.PodPending {
				continue
			}
			addEvent(&mount, e)
			if mount.Message == "" {
				mount.Message = firstLine(e.Message)
			}
		case e.Reason == "FailedScheduling":
			addEvent(&sched, e)
			if sched.Message == "" {
				sched.Message = "No node can run it: " + firstLine(e.Message)
			}
		}
	}
	var out []Found
	if probe.Evidence != nil {
		probe.Type = "probe_failing"
		var unready []string
		for _, s := range pod.Status.ContainerStatuses {
			if s.State.Running != nil && !s.Ready {
				unready = append(unready, s.Name)
			}
		}
		addLogs(&probe, c.Logs(string(pod.UID)), unready, false)
		out = append(out, probe)
	}
	if mount.Evidence != nil {
		mount.Type = "mount_failed"
		out = append(out, mount)
	}
	// FailedScheduling events only add to the condition's signal: an old
	// one about a pod that has since been scheduled says nothing.
	if sched.Evidence != nil && !scheduled(pod) {
		sched.Type = "unschedulable"
		out = append(out, sched)
	}
	return out
}

// addLogs adds what the containers' logs said to a signal: a piece of
// evidence per pattern that matched, with how many lines, one for the
// other kinds of line, or why the log couldn't be read. Only a finding from
// the run the signal is about counts (the previous run for a crash loop).
// The message gets the first pattern, in words: "…; its log shows
// connection refused".
func addLogs(f *Found, findings map[string]LogFinding, containers []string, previous bool) {
	said := ""
	for _, name := range containers {
		lf, ok := findings[name]
		if !ok || lf.Previous != previous {
			continue
		}
		where := "log of container " + name
		if previous {
			where += ", previous run"
		}
		if lf.Error != "" {
			f.Evidence = append(f.Evidence, Evidence{Source: FromLog, Field: where, Value: "couldn't be read: " + lf.Error, Seen: lf.Read})
			continue
		}
		for _, p := range logs.Patterns {
			if n := lf.Patterns[p.Name]; n > 0 {
				f.Evidence = append(f.Evidence, Evidence{Source: FromLog, Field: where, Pattern: p.Name, Matches: n, Seen: lf.Read})
				if said == "" {
					said = p.About
				}
			}
		}
		if lf.Other > 0 {
			f.Evidence = append(f.Evidence, Evidence{Source: FromLog, Field: where,
				Value: fmt.Sprintf("%d other %s of line, not kept", lf.Other, plural(int64(lf.Other), "kind", "kinds")), Seen: lf.Read})
		}
	}
	if said != "" {
		f.Message += "; its log shows " + said
	}
}

// addEvent records an event as evidence, counts it, and moves Since to the
// earliest first time any of them happened.
func addEvent(f *Found, e *corev1.Event) {
	f.Evidence = append(f.Evidence, Evidence{Source: FromEvent,
		Event: &EventRef{UID: string(e.UID), Reason: e.Reason, Count: eventCount(e), LastSeen: eventTime(e)}})
	f.Count += int64(eventCount(e))
	if first := eventFirst(e); !first.IsZero() && (f.Since.IsZero() || first.Before(f.Since)) {
		f.Since = first
	}
}

func runningNotReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, s := range pod.Status.ContainerStatuses {
		if s.State.Running != nil && !s.Ready {
			return true
		}
	}
	return false
}

func scheduled(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return pod.Spec.NodeName != ""
}

func ownedBy(obj metav1.Object, kind string) bool {
	c := metav1.GetControllerOfNoCopy(obj)
	return c != nil && c.Kind == kind
}

func imageOf(pod *corev1.Pod, container string) string {
	for _, list := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, c := range list {
			if c.Name == container {
				return c.Image
			}
		}
	}
	return "?"
}

// limitText: " (limit 16Mi)", or "" without a memory limit.
func limitText(pod *corev1.Pod, container string) string {
	for _, list := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, c := range list {
			if q, ok := c.Resources.Limits[corev1.ResourceMemory]; c.Name == container && ok {
				return " (limit " + q.String() + ")"
			}
		}
	}
	return ""
}

// crashMessage: "Container app restarted 14 times; last exit Error (exit 1)".
func crashMessage(pod *corev1.Pod, names []string, restarts int64) string {
	msg := fmt.Sprintf("%s restarted %d %s", who(names, false), restarts, plural(restarts, "time", "times"))
	for _, s := range pod.Status.ContainerStatuses {
		if s.Name != names[0] {
			continue
		}
		last := s.LastTerminationState.Terminated
		if s.State.Terminated != nil {
			last = s.State.Terminated // the exit that just happened
		}
		if last != nil {
			msg += "; last exit " + exitText(last)
		}
	}
	return msg
}

// backoffReset is how long a container must run for the kubelet to forget
// its back-off: one that exits sooner is still crash-looping.
const backoffReset = 10 * time.Minute

// restartsSoon: a container that has restarted before, has just exited
// after a short run, and will be started again. That's a crash loop the
// kubelet hasn't put back in CrashLoopBackOff yet. One that ran for longer
// than backoffReset before exiting isn't: it's one restart.
func restartsSoon(pod *corev1.Pod, s corev1.ContainerStatus, t *corev1.ContainerStateTerminated) bool {
	if s.RestartCount < 1 || pod.Spec.RestartPolicy == corev1.RestartPolicyNever ||
		pod.Spec.RestartPolicy == corev1.RestartPolicyOnFailure && t.ExitCode == 0 {
		return false
	}
	return t.FinishedAt.Sub(t.StartedAt.Time) < backoffReset
}

// exitText: "Error (exit 1)", "OOMKilled (exit 137)", "exit 2".
func exitText(t *corev1.ContainerStateTerminated) string {
	if t.Reason == "" {
		return fmt.Sprintf("exit %d", t.ExitCode)
	}
	return fmt.Sprintf("%s (exit %d)", t.Reason, t.ExitCode)
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// firstLine keeps a message to its first line and 200 characters: some
// probes' messages carry a whole HTTP response, and the scheduler's list
// every node. An exec probe that printed nothing leaves "failed: ", which
// loses its colon.
func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	s = strings.TrimRight(s, ": ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func unique(names []string) []string {
	var out []string
	for _, n := range names {
		if len(out) == 0 || out[len(out)-1] != n {
			out = append(out, n)
		}
	}
	return out
}

// eventCount and eventFirst read an event's count and first time from
// whichever fields its reporter used, as eventTime does for the last.
func eventCount(e *corev1.Event) int32 {
	switch {
	case e.Series != nil && e.Series.Count > 0:
		return e.Series.Count
	case e.Count > 0:
		return e.Count
	}
	return 1
}

func eventFirst(e *corev1.Event) time.Time {
	switch {
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}
