package diagnose

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"opscope/internal/graph"
	"opscope/internal/kube"
	"opscope/internal/snapshot"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// container is one container of a pod with its status.
type container struct {
	spec   corev1.Container
	status corev1.ContainerStatus
	init   bool
}

// sidecar says whether it's a native sidecar: an init container that keeps
// running next to the others (restartPolicy: Always on the container).
func (c container) sidecar(pod *corev1.Pod) bool {
	return c.init && kube.IsSidecar(pod, c.spec.Name)
}

// name is how a finding refers to it: "Container api", "Init container migrate".
func (c container) name() string {
	if c.init {
		return "Init container " + c.spec.Name
	}
	return "Container " + c.spec.Name
}

func containersOf(pod *corev1.Pod) []container {
	statuses := map[string]corev1.ContainerStatus{}
	for _, s := range append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...) {
		statuses[s.Name] = s
	}
	var containers []container
	for _, c := range pod.Spec.InitContainers {
		containers = append(containers, container{spec: c, status: statuses[c.Name], init: true})
	}
	for _, c := range pod.Spec.Containers {
		containers = append(containers, container{spec: c, status: statuses[c.Name]})
	}
	return containers
}

func podOf(c *Context, n *graph.Node) *corev1.Pod {
	pod, _ := c.Object(n).(*corev1.Pod)
	return pod
}

// ---- pod.config: a container can't be created from its configuration ----

var (
	missingObjectRE = regexp.MustCompile(`(configmap|secret) "([^"]+)" not found`)
	missingKeyRE    = regexp.MustCompile(`couldn't find key (\S+) in (ConfigMap|Secret) ([^/\s]+)/(\S+)`)
)

var podConfig = Rule{ID: "pod.config", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil {
		return nil
	}
	var findings []*Finding
	for _, ct := range containersOf(pod) {
		w := ct.status.State.Waiting
		if w == nil || w.Reason != "CreateContainerConfigError" {
			continue
		}
		f := &Finding{
			Severity:   Failing,
			Confidence: Certain,
			Evidence:   []Evidence{{Source: "container", Text: fmt.Sprintf("%s: CreateContainerConfigError: %s", ct.spec.Name, w.Message)}},
			Next:       []Step{kubectl("describe", n)},
		}
		if m := missingKeyRE.FindStringSubmatch(w.Message); m != nil {
			key, kind, namespace, name := m[1], m[2], m[3], m[4]
			target := c.Node(snapshot.Key{Resource: strings.ToLower(kind) + "s", Namespace: namespace, Name: name}.String())
			f.Title = fmt.Sprintf("%s %s has no key %s", kind, name, key)
			f.Detail = fmt.Sprintf("%s reads %s from it%s. Add the key to the %s, or change the reference.",
				ct.name(), key, envNames(ct.spec, name, key), kind)
			if target != nil {
				f.Blames = []string{target.ID}
			}
		} else if m := missingObjectRE.FindStringSubmatch(w.Message); m != nil {
			resource, name := m[1]+"s", m[2]
			target := c.Node(snapshot.Key{Resource: resource, Namespace: pod.Namespace, Name: name}.String())
			kind := map[string]string{"configmaps": "ConfigMap", "secrets": "Secret"}[resource]
			f.Title = fmt.Sprintf("%s can't start: it needs %s %s", ct.name(), kind, name)
			f.Detail = "Kubernetes can't find it, so the container isn't created."
			if target != nil {
				f.Blames = []string{target.ID}
			}
		} else {
			f.Title = ct.name() + " can't be created"
			f.Detail = w.Message
		}
		f.Evidence = append(f.Evidence, eventEvidence(c.Events(pod.UID, "Failed"))...)
		findings = append(findings, f)
	}
	return findings
}}

// envNames says which environment variables read a key: " (env DATABASE_URL)".
func envNames(spec corev1.Container, object, key string) string {
	var names []string
	for _, env := range spec.Env {
		if env.ValueFrom == nil {
			continue
		}
		if ref := env.ValueFrom.ConfigMapKeyRef; ref != nil && ref.Name == object && ref.Key == key {
			names = append(names, env.Name)
		}
		if ref := env.ValueFrom.SecretKeyRef; ref != nil && ref.Name == object && ref.Key == key {
			names = append(names, env.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " (env " + strings.Join(names, ", ") + ")"
}

// ---- pod.mount: a volume can't be mounted ----

// starting is every status of a pod that hasn't started its containers.
var startingRE = regexp.MustCompile(`^(Pending|ContainerCreating|PodInitializing|Init:\d+/\d+)$`)

var podMount = Rule{ID: "pod.mount", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil || !startingRE.MatchString(n.Status) || pod.Spec.NodeName == "" {
		return nil // running, or not scheduled yet: mounting isn't the problem
	}
	mountEvents := c.Events(pod.UID, "FailedMount", "FailedAttachVolume")

	var findings []*Finding
	for _, v := range pod.Spec.Volumes {
		for _, target := range volumeTargets(c, pod.Namespace, v) {
			problem := ""
			switch {
			case target.State == graph.Missing:
				problem = "which doesn't exist"
			case target.State == graph.Unknown:
				problem = "which Opscope can't check"
			case target.Resource == "persistentvolumeclaims" && target.Status != "Bound":
				problem = "which isn't bound to a volume yet"
			default:
				continue
			}
			var events []*corev1.Event
			for _, e := range mountEvents {
				if strings.Contains(e.Message, `volume "`+v.Name+`"`) {
					events = append(events, e)
				}
			}
			findings = append(findings, &Finding{
				Severity:   Failing,
				Confidence: Certain,
				Title:      fmt.Sprintf("Volume %s can't be mounted", v.Name),
				Detail:     fmt.Sprintf("It needs %s, %s. The pod waits in %s until it can be mounted.", describe(target, pod.Namespace), problem, n.Status),
				Evidence:   eventEvidence(events),
				Blames:     []string{target.ID},
				Next:       []Step{eventsOf(n)},
			})
		}
	}
	if len(findings) == 0 && len(mountEvents) > 0 {
		// Nothing in the snapshot explains it: report what the kubelet says.
		findings = append(findings, &Finding{
			Severity:   Failing,
			Confidence: Likely,
			Title:      "A volume can't be mounted",
			Detail:     mountEvents[0].Message,
			Evidence:   eventEvidence(mountEvents),
			Next:       []Step{eventsOf(n), kubectl("describe", n)},
		})
	}
	return findings
}}

// volumeTargets returns the graph nodes a volume needs (its ConfigMap,
// Secret or claim). Optional references to nothing aren't in the graph, so
// they never show up here.
func volumeTargets(c *Context, namespace string, v corev1.Volume) []*graph.Node {
	var keys []snapshot.Key
	in := func(resource, name string) snapshot.Key {
		return snapshot.Key{Resource: resource, Namespace: namespace, Name: name}
	}
	switch {
	case v.ConfigMap != nil:
		keys = append(keys, in("configmaps", v.ConfigMap.Name))
	case v.Secret != nil:
		keys = append(keys, in("secrets", v.Secret.SecretName))
	case v.PersistentVolumeClaim != nil:
		keys = append(keys, in("persistentvolumeclaims", v.PersistentVolumeClaim.ClaimName))
	case v.Projected != nil:
		for _, source := range v.Projected.Sources {
			if source.ConfigMap != nil {
				keys = append(keys, in("configmaps", source.ConfigMap.Name))
			}
			if source.Secret != nil {
				keys = append(keys, in("secrets", source.Secret.Name))
			}
		}
	}
	var nodes []*graph.Node
	for _, k := range keys {
		if node := c.Node(k.String()); node != nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// ---- pod.image: an image can't be pulled ----

var podImage = Rule{ID: "pod.image", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil {
		return nil
	}
	var findings []*Finding
	for _, ct := range containersOf(pod) {
		w := ct.status.State.Waiting
		if w == nil || (w.Reason != "ErrImagePull" && w.Reason != "ImagePullBackOff" && w.Reason != "InvalidImageName") {
			continue
		}
		image := ct.spec.Image
		f := &Finding{
			Severity:   Failing,
			Confidence: Likely,
			Evidence:   []Evidence{{Source: "container", Text: fmt.Sprintf("%s: %s: %s", ct.spec.Name, w.Reason, w.Message)}},
			Next:       []Step{eventsOf(n)},
		}
		if w.Reason == "InvalidImageName" {
			f.Confidence = Certain
			f.Title = fmt.Sprintf("%s's image name %q isn't valid", ct.name(), image)
			f.Detail = w.Message
			findings = append(findings, f)
			continue
		}

		// The waiting message is only "Back-off pulling image"; the reason is
		// in the kubelet's "Failed to pull image" event.
		message := w.Message
		var pulls []*corev1.Event
		for _, e := range c.Events(pod.UID, "Failed") {
			if strings.Contains(e.Message, "Failed to pull image") && strings.Contains(e.Message, image) {
				pulls = append(pulls, e)
			}
		}
		if len(pulls) > 0 {
			message = pulls[0].Message
		}
		f.Evidence = append(f.Evidence, eventEvidence(pulls)...)
		registry := registryOf(image)

		switch pullProblem(message) {
		case "unreachable":
			f.Title = fmt.Sprintf("The registry %s can't be reached", registry)
			f.Detail = fmt.Sprintf("%s's image is %s. The node can't connect to %s: the name may be wrong, or the network or a firewall blocks it.", ct.name(), image, registry)
		case "unauthorized":
			f.Title = fmt.Sprintf("The registry %s refuses to give image %s", registry, image)
			secrets := pullSecrets(c, n)
			if len(pod.Spec.ImagePullSecrets) == 0 {
				f.Detail = "It needs credentials, and the pod has no imagePullSecrets."
			} else {
				f.Detail = "It needs credentials. The pod's imagePullSecrets may be wrong, expired, or for another registry."
				f.Blames = secrets
			}
		case "notFound":
			f.Title = fmt.Sprintf("Image %s doesn't exist", image)
			f.Detail = fmt.Sprintf("The registry %s has no such image or tag. Check the name and tag in %s's spec.", registry, ct.name())
		case "notFoundOrDenied":
			f.Title = fmt.Sprintf("Image %s doesn't exist, or needs credentials", image)
			f.Detail = fmt.Sprintf("The registry %s says the repository doesn't exist or needs authorization; it doesn't say which.", registry)
		default:
			f.Title = fmt.Sprintf("Image %s can't be pulled", image)
			f.Detail = message
		}
		findings = append(findings, f)
	}
	return findings
}}

// pullProblem sorts an image pull error by what the message says. The
// wording comes from the container runtime and the registry, so this reads
// it as best it can.
func pullProblem(message string) string {
	m := strings.ToLower(message)
	hasAny := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(m, w) {
				return true
			}
		}
		return false
	}
	switch {
	case hasAny("no such host", "i/o timeout", "connection refused", "tls handshake", "x509:", "network is unreachable", "dial tcp"):
		return "unreachable"
	case hasAny("repository does not exist or may require"):
		return "notFoundOrDenied"
	case hasAny("unauthorized", "401", "403", "forbidden", "authorization failed", "no basic auth credentials", "denied"):
		return "unauthorized"
	case hasAny("notfound", "not found", "manifest unknown"):
		return "notFound"
	}
	return ""
}

// registryOf is the registry an image comes from: the first part of the
// name when it looks like a host, otherwise Docker Hub.
func registryOf(image string) string {
	first, _, found := strings.Cut(image, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// pullSecrets returns the pod's imagePullSecrets that are missing or
// unknown: those are worth blaming. Ones that exist may still be wrong, but
// nothing here can tell.
func pullSecrets(c *Context, n *graph.Node) []string {
	var blamed []string
	for _, e := range c.G.Out(n.ID) {
		if e.Type == graph.Uses && slices.Contains(e.Via, "imagePullSecret") {
			if target := c.Node(e.To); target.State != graph.OK {
				blamed = append(blamed, target.ID)
			}
		}
	}
	return blamed
}

// ---- pod.crash: a container keeps stopping ----

// exitMeanings explains exit codes that mean the same in any program.
var exitMeanings = map[int32]string{
	126: "the command can't be run (not executable)",
	127: "the command isn't found in the image",
	137: "it was killed (SIGKILL)",
	139: "it crashed (segmentation fault)",
	143: "it was stopped (SIGTERM)",
}

// recentRestart is how long ago a container that runs fine again may have
// stopped and still count as restarting. A failing liveness probe restarts
// a container every few minutes; a node reboot last night doesn't matter now.
const recentRestart = time.Hour

var podCrash = Rule{ID: "pod.crash", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil || pod.Status.Reason == "Evicted" {
		return nil // its containers were stopped by the eviction; pod.evicted says why
	}
	var findings []*Finding
	for _, ct := range containersOf(pod) {
		s := ct.status
		crashLooping := s.State.Waiting != nil && s.State.Waiting.Reason == "CrashLoopBackOff"
		last := s.LastTerminationState.Terminated
		if s.State.Terminated != nil && s.State.Terminated.ExitCode != 0 {
			last = s.State.Terminated
		}
		// A container that failed and won't be started again (a Job's pod,
		// restartPolicy: Never) is as much a problem as one that keeps failing.
		final := s.RestartCount == 0 && s.State.Terminated != nil && s.State.Terminated.ExitCode != 0 &&
			pod.Spec.RestartPolicy != corev1.RestartPolicyAlways && !ct.sidecar(pod)
		if last == nil || (!crashLooping && s.RestartCount == 0 && !final) {
			continue
		}
		// Running again, and the last stop was a while ago: it restarted (a
		// node reboot, an upgrade) and hasn't stopped since. If it isn't ready,
		// pod.not-ready says so.
		if s.State.Running != nil && c.Now.Sub(last.FinishedAt.Time) > recentRestart {
			continue
		}

		f := &Finding{
			Severity:   Degraded,
			Confidence: Certain,
			Evidence: []Evidence{{Source: "container", Text: fmt.Sprintf("%s: restarted %s; last stopped: %s (exit %d)",
				ct.spec.Name, plural(int(s.RestartCount), "time", "times"), orElse(last.Reason, "Error"), last.ExitCode)}},
			Next: []Step{
				{Label: "Previous logs", Object: n.ID, Tab: "logs", Query: "container=" + ct.spec.Name + "&previous=true"},
				kubectl("logs", n, "-c", ct.spec.Name, "--previous"),
			},
		}
		if final {
			f.Evidence[0].Text = fmt.Sprintf("%s: stopped: %s (exit %d); restartPolicy: %s", ct.spec.Name, orElse(last.Reason, "Error"), last.ExitCode, pod.Spec.RestartPolicy)
			f.Next = []Step{
				{Label: "Logs", Object: n.ID, Tab: "logs", Query: "container=" + ct.spec.Name},
				kubectl("logs", n, "-c", ct.spec.Name),
			}
		}
		if last.FinishedAt.Time.After(last.StartedAt.Time) {
			f.Evidence[0].Time = &last.FinishedAt.Time
		}
		if crashLooping || final || n.State == graph.Failing {
			f.Severity = Failing
		}
		if last.Message != "" {
			f.Evidence = append(f.Evidence, Evidence{Source: "container", Text: "Termination message: " + last.Message})
		}

		probe := livenessFailures(c, pod, ct.spec.Name)
		switch {
		case last.Reason == "OOMKilled":
			f.Title = ct.name() + " runs out of memory"
			if limit := ct.spec.Resources.Limits.Memory(); limit != nil && !limit.IsZero() {
				f.Detail = fmt.Sprintf("It was stopped when it reached its memory limit of %s. Raise the limit, or find out why it uses that much.", limit.String())
				f.Evidence = append(f.Evidence, Evidence{Source: "spec", Text: "limits.memory: " + limit.String()})
			} else {
				f.Detail = "It has no memory limit, and was stopped when the node ran short of memory."
			}
		case len(probe) > 0 && (last.ExitCode == 137 || last.ExitCode == 143):
			f.Title = ct.name() + " fails its liveness probe, so it's restarted"
			f.Detail = "Kubernetes restarts a container whose liveness probe fails. " + describeProbe(ct.spec.LivenessProbe) +
				" Check that the probe matches what the program offers, and that its delay leaves enough time to start."
			f.Confidence = Likely
			f.Evidence = append(f.Evidence, eventEvidence(probe)...)
			f.Next = []Step{eventsOf(n), kubectl("describe", n)}
		case final:
			f.Title = fmt.Sprintf("%s exited with code %d", ct.name(), last.ExitCode)
			f.Detail = fmt.Sprintf("It won't be started again (restartPolicy: %s). Its logs usually say why it failed.", pod.Spec.RestartPolicy)
			if meaning, ok := exitMeanings[last.ExitCode]; ok {
				f.Detail = fmt.Sprintf("Exit code %d means %s. It won't be started again (restartPolicy: %s).", last.ExitCode, meaning, pod.Spec.RestartPolicy)
			}
		case last.ExitCode == 0:
			f.Title = ct.name() + " exits as soon as it starts"
			f.Detail = "It finishes with exit code 0, but the pod expects it to keep running, so it's started again and again. Its command may be wrong, or it may be meant for a Job."
		default:
			f.Title = fmt.Sprintf("%s exits with code %d", ct.name(), last.ExitCode)
			if meaning, ok := exitMeanings[last.ExitCode]; ok {
				f.Detail = fmt.Sprintf("Exit code %d means %s.", last.ExitCode, meaning)
			} else {
				f.Detail = "The program in it stops with an error. Its logs from before the restart usually say why."
			}
		}
		f.Evidence = append(f.Evidence, eventEvidence(c.Events(pod.UID, "BackOff"))...)
		findings = append(findings, f)
	}
	return findings
}}

// livenessFailures returns the events that say a container's liveness
// probe failed or that it was killed for it.
func livenessFailures(c *Context, pod *corev1.Pod, name string) []*corev1.Event {
	var events []*corev1.Event
	for _, e := range c.Events(pod.UID, "Unhealthy", "Killing") {
		if strings.HasPrefix(e.Message, "Liveness probe failed") || strings.Contains(e.Message, "failed liveness probe") {
			if e.InvolvedObject.FieldPath == "" || strings.Contains(e.InvolvedObject.FieldPath, "{"+name+"}") {
				events = append(events, e)
			}
		}
	}
	return events
}

func describeProbe(p *corev1.Probe) string {
	if p == nil {
		return ""
	}
	var what string
	switch {
	case p.HTTPGet != nil:
		what = fmt.Sprintf("an HTTP GET of %s on port %s", orElse(p.HTTPGet.Path, "/"), p.HTTPGet.Port.String())
	case p.TCPSocket != nil:
		what = "a connection to port " + p.TCPSocket.Port.String()
	case p.Exec != nil:
		what = "the command " + strings.Join(p.Exec.Command, " ")
	case p.GRPC != nil:
		what = fmt.Sprintf("a gRPC health check on port %d", p.GRPC.Port)
	default:
		return ""
	}
	// The API server fills in these defaults; a probe written by hand in a
	// test may not have them.
	period, threshold := p.PeriodSeconds, p.FailureThreshold
	if period == 0 {
		period = 10
	}
	if threshold == 0 {
		threshold = 3
	}
	return fmt.Sprintf("This one is %s, every %ds after an initial %ds, giving up after %s.",
		what, period, p.InitialDelaySeconds, plural(int(threshold), "failure", "failures"))
}

// ---- pod.unschedulable: no node can run the pod ----

var (
	availableRE = regexp.MustCompile(`\d+/\d+ nodes are available: (.*?)\.(?: |$)`)
	countedRE   = regexp.MustCompile(`^(\d+) (.*)$`)
)

var podUnschedulable = Rule{ID: "pod.unschedulable", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil || pod.Spec.NodeName != "" {
		return nil
	}
	var cond *corev1.PodCondition
	for i := range pod.Status.Conditions {
		if pc := &pod.Status.Conditions[i]; pc.Type == corev1.PodScheduled && pc.Status == corev1.ConditionFalse && pc.Reason == "Unschedulable" {
			cond = pc
		}
	}
	if cond == nil {
		return nil
	}

	f := &Finding{
		Severity:   Failing,
		Confidence: Certain,
		Title:      "No node can run it",
		Evidence: append([]Evidence{conditionEvidence("PodScheduled", "False", cond.Reason, cond.Message, cond.LastTransitionTime)},
			eventEvidence(c.Events(pod.UID, "FailedScheduling"))...),
		Next: []Step{eventsOf(n), {Label: "kubectl get nodes", Command: "kubectl get nodes"}},
	}

	var reasons []string
	if m := availableRE.FindStringSubmatch(cond.Message); m != nil {
		for _, part := range strings.Split(m[1], ", ") {
			reasons = append(reasons, c.schedulingReason(pod, n, part, f))
		}
	}
	if len(reasons) == 0 {
		f.Confidence = Likely
		f.Detail = cond.Message
	} else {
		f.Detail = strings.Join(reasons, " ") + " Until one can, it waits in Pending."
	}
	return []*Finding{f}
}}

// schedulingReason turns one part of the scheduler's message ("2
// Insufficient cpu") into a sentence, blaming claims that aren't bound.
func (c *Context) schedulingReason(pod *corev1.Pod, n *graph.Node, part string, f *Finding) string {
	count, phrase := "", part
	if m := countedRE.FindStringSubmatch(part); m != nil {
		count, phrase = m[1], m[2]
	}
	nodes := "Nodes"
	switch count {
	case "":
	case "1":
		nodes = "1 node"
	default:
		nodes = count + " nodes"
	}
	verb := func(one, many string) string {
		if count == "1" {
			return one
		}
		return many
	}

	switch {
	case strings.HasPrefix(phrase, "Insufficient "):
		what := strings.TrimPrefix(phrase, "Insufficient ")
		sentence := fmt.Sprintf("%s %s enough free %s.", nodes, verb("doesn't have", "don't have"), resourceName(what))
		if asks, most := requestAndLargest(c.S, pod, corev1.ResourceName(what)); asks != "" {
			sentence = fmt.Sprintf("%s %s enough free %s: it asks for %s, and the largest node can give %s.",
				nodes, verb("doesn't have", "don't have"), resourceName(what), asks, most)
		}
		return sentence
	case strings.Contains(phrase, "untolerated taint"):
		taint := phrase[strings.Index(phrase, "taint")+len("taint"):]
		return fmt.Sprintf("%s %s a taint it doesn't tolerate:%s.", nodes, verb("has", "have"), taint)
	case strings.Contains(phrase, "didn't match Pod's node affinity/selector"):
		return fmt.Sprintf("%s %s match its node selector or affinity.", nodes, verb("doesn't", "don't"))
	case strings.Contains(phrase, "were unschedulable") || strings.Contains(phrase, "was unschedulable"):
		return fmt.Sprintf("%s %s cordoned.", nodes, verb("is", "are"))
	case strings.Contains(phrase, "didn't have free ports"):
		return fmt.Sprintf("%s %s have the host port it needs free.", nodes, verb("doesn't", "don't"))
	case strings.Contains(phrase, "volume node affinity conflict"):
		return fmt.Sprintf("%s can't reach its volumes.", nodes)
	case strings.Contains(phrase, "unbound immediate PersistentVolumeClaims"):
		for _, e := range c.G.Out(n.ID) {
			if target := c.Node(e.To); target.Resource == "persistentvolumeclaims" && target.Status != "Bound" {
				f.Blames = append(f.Blames, target.ID)
			}
		}
		return "Its PersistentVolumeClaims aren't bound to volumes yet."
	}
	return nodes + ": " + phrase + "."
}

func resourceName(name string) string {
	switch name {
	case "cpu":
		return "CPU"
	case "memory":
		return "memory"
	}
	return name
}

// requestAndLargest says how much of a resource a pod asks for, and the
// most any node can give (its allocatable amount), both formatted.
func requestAndLargest(s *snapshot.Snapshot, pod *corev1.Pod, name corev1.ResourceName) (string, string) {
	total := resource.Quantity{}
	for _, c := range pod.Spec.Containers {
		if q, ok := c.Resources.Requests[name]; ok {
			total.Add(q)
		}
	}
	if total.IsZero() || len(s.Nodes) == 0 {
		return "", ""
	}
	var amounts []resource.Quantity
	for _, node := range s.Nodes {
		amounts = append(amounts, node.Status.Allocatable[name])
	}
	sort.Slice(amounts, func(i, j int) bool { return amounts[i].Cmp(amounts[j]) > 0 })
	return quantity(total, name), quantity(amounts[0], name)
}

func quantity(q resource.Quantity, name corev1.ResourceName) string {
	if name == corev1.ResourceCPU {
		if q.MilliValue()%1000 == 0 {
			return fmt.Sprintf("%d CPU", q.Value())
		}
		return fmt.Sprintf("%dm CPU", q.MilliValue())
	}
	if name == corev1.ResourceMemory {
		return fmt.Sprintf("%.1f GiB", float64(q.Value())/(1<<30))
	}
	return q.String()
}

// ---- pod.node: the pod's node is down ----

var podNodeDown = Rule{ID: "pod.node", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	for _, e := range c.G.Out(n.ID) {
		if node := c.Node(e.To); e.Type == graph.RunsOn && node.State == graph.Failing {
			return []*Finding{{
				Severity:   Failing,
				Confidence: Certain,
				Title:      fmt.Sprintf("Its node %s isn't Ready", node.Name),
				Detail: "While its node is down, the pod can't run, whatever status it last reported. " +
					"If the node doesn't come back, the pod is removed after a few minutes and, if a workload owns it, started elsewhere.",
				Blames: []string{node.ID},
			}}
		}
	}
	return nil
}}

func orElse(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// ---- pod.not-ready: running, but not ready ----

var podNotReady = Rule{ID: "pod.not-ready", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		return nil
	}
	var findings []*Finding
	for _, ct := range containersOf(pod) {
		if ct.status.State.Running == nil || ct.status.Ready || (ct.init && !ct.sidecar(pod)) {
			continue
		}
		f := &Finding{
			Severity:   Info, // just started: give it time
			Confidence: Certain,
			Title:      ct.name() + " isn't ready",
			Detail:     "It runs, but Kubernetes sends it no traffic until it's ready.",
			Evidence:   []Evidence{{Source: "container", Text: ct.spec.Name + ": running, not ready"}},
			Next:       []Step{eventsOf(n), kubectl("describe", n)},
		}
		if n.State == graph.Degraded || n.State == graph.Failing {
			f.Severity = Degraded
		}
		var probe []*corev1.Event
		for _, e := range c.Events(pod.UID, "Unhealthy") {
			if strings.HasPrefix(e.Message, "Readiness probe failed") &&
				(e.InvolvedObject.FieldPath == "" || strings.Contains(e.InvolvedObject.FieldPath, "{"+ct.spec.Name+"}")) {
				probe = append(probe, e)
			}
		}
		if ct.spec.ReadinessProbe != nil {
			f.Title = ct.name() + " fails its readiness probe"
			f.Detail = "It runs, but Kubernetes sends it no traffic until its readiness probe passes. " + describeProbe(ct.spec.ReadinessProbe)
			f.Evidence = append(f.Evidence, eventEvidence(probe)...)
		}
		findings = append(findings, f)
	}
	return findings
}}

// ---- pod.evicted: the kubelet evicted the pod ----

var podEvicted = Rule{ID: "pod.evicted", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil || pod.Status.Reason != "Evicted" {
		return nil
	}
	said := strings.TrimSpace(pod.Status.Message)
	f := &Finding{
		Severity:   Failing,
		Confidence: Certain,
		Title:      "It was evicted",
		Detail:     said,
		Evidence:   []Evidence{{Source: "status", Text: "Evicted: " + said}},
		Next:       []Step{kubectl("describe", n)},
	}
	message := strings.ToLower(said)
	switch {
	case strings.Contains(message, "the node was low on resource"):
		resource := "resources"
		if m := lowOnRE.FindStringSubmatch(pod.Status.Message); m != nil {
			resource = m[1]
		}
		f.Title = fmt.Sprintf("It was evicted: its node ran low on %s", resource)
		f.Detail = "The kubelet evicts pods to keep a node working when it runs short. " + said
		if node := c.Node(snapshot.Key{Resource: "nodes", Name: pod.Spec.NodeName}.String()); node != nil {
			f.Blames = []string{node.ID}
		}
	case strings.Contains(message, "ephemeral local storage"):
		f.Title = "It was evicted for using more disk than its limit"
		f.Detail = said + " Raise its ephemeral-storage limit, or write less to the container's own disk (use a volume)."
	}
	// A workload has made a new pod in its place already; this one is what's
	// left of the old one.
	if topOwner(c.G, n.ID) != n.ID {
		f.Severity = Info
		f.Detail += " Its workload has replaced it; Kubernetes keeps evicted pods for a while, for their status."
	}
	return []*Finding{f}
}}

var lowOnRE = regexp.MustCompile(`low on resource: ([a-z-]+)`)

// ---- pod.init: an init container doesn't finish ----

var podInit = Rule{ID: "pod.init", Resources: []string{"pods"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pod := podOf(c, n)
	if pod == nil || n.State != graph.Failing || !strings.HasPrefix(n.Status, "Init:") {
		return nil
	}
	for _, ct := range containersOf(pod) {
		if !ct.init || ct.sidecar(pod) || ct.status.State.Running == nil {
			continue
		}
		since := ct.status.State.Running.StartedAt.Time
		return []*Finding{{
			Severity:   Failing,
			Confidence: Certain,
			Title:      fmt.Sprintf("%s is still running after %s", ct.name(), ago(c.Now.Sub(since))),
			Detail: "The pod's other containers start only when every init container has finished. " +
				"An init container that runs this long is usually waiting for something; its logs say what.",
			Evidence: []Evidence{{Source: "container", Text: fmt.Sprintf("%s: running since %s", ct.spec.Name, since.UTC().Format(time.RFC3339)), Time: &since}},
			Next: []Step{
				{Label: "Logs", Object: n.ID, Tab: "logs", Query: "container=" + ct.spec.Name},
				kubectl("logs", n, "-c", ct.spec.Name),
			},
		}}
	}
	return nil
}}
