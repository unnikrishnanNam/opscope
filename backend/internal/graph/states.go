package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"opscope/internal/kube"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
)

// addStates works out every node's state. Pods come first, since the
// state of a workload depends on its pods.
func (b *builder) addStates() {
	s := b.s
	for i := range s.Nodes {
		n := &s.Nodes[i]
		state, status, message := nodeState(n)
		b.set(key("nodes", n), state, status, message)
	}
	for i := range s.Namespaces {
		ns := &s.Namespaces[i]
		state := OK
		if ns.Status.Phase == corev1.NamespaceTerminating {
			state = Progressing
		}
		b.set(key("namespaces", ns), state, string(ns.Status.Phase), "")
	}
	for i := range s.Pods {
		pod := &s.Pods[i]
		state, status, message := b.podState(pod)
		b.set(key("pods", pod), state, status, message)
	}

	for i := range s.ReplicaSets {
		rs := &s.ReplicaSets[i]
		desired := replicaCount(rs.Spec.Replicas)
		r := replicas{desired: desired, ready: rs.Status.ReadyReplicas, updated: desired} // a ReplicaSet has one template
		r.failure = conditionMessage(rsCondition(rs.Status.Conditions, appsv1.ReplicaSetReplicaFailure))
		b.setReplicas(key("replicasets", rs), r, b.podStates(rs.UID))
		if r.failure != "" {
			b.g.cantCreatePods[key("replicasets", rs)] = true
		}
	}
	for i := range s.Deployments {
		d := &s.Deployments[i]
		r := replicas{desired: replicaCount(d.Spec.Replicas), ready: d.Status.ReadyReplicas, updated: d.Status.UpdatedReplicas}
		r.rolling = d.Status.ObservedGeneration < d.Generation
		for _, c := range d.Status.Conditions {
			switch {
			case c.Type == appsv1.DeploymentProgressing && c.Reason == "ProgressDeadlineExceeded":
				r.failure = c.Message
			case c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue:
				r.failure = c.Message
			}
		}
		var pods []State
		for _, k := range s.Owned(d.UID) {
			if obj, ok := s.Get(k.Resource, k.Namespace, k.Name); ok && k.Resource == "replicasets" {
				rs := obj.(*appsv1.ReplicaSet)
				pods = append(pods, b.podStates(rs.UID)...)
				// A ReplicaSet that can't create pods says why; that's the Deployment's problem too.
				if failure := conditionMessage(rsCondition(rs.Status.Conditions, appsv1.ReplicaSetReplicaFailure)); failure != "" && r.failure == "" {
					r.failure = failure
				}
			}
		}
		b.setReplicas(key("deployments", d), r, pods)
	}
	for i := range s.StatefulSets {
		ss := &s.StatefulSets[i]
		r := replicas{desired: replicaCount(ss.Spec.Replicas), ready: ss.Status.ReadyReplicas, updated: ss.Status.UpdatedReplicas}
		r.rolling = ss.Status.ObservedGeneration < ss.Generation
		b.setReplicas(key("statefulsets", ss), r, b.podStates(ss.UID))
	}
	for i := range s.DaemonSets {
		ds := &s.DaemonSets[i]
		r := replicas{desired: ds.Status.DesiredNumberScheduled, ready: ds.Status.NumberReady, updated: ds.Status.UpdatedNumberScheduled}
		r.rolling = ds.Status.ObservedGeneration < ds.Generation
		b.setReplicas(key("daemonsets", ds), r, b.podStates(ds.UID))
	}
	for i := range s.Jobs {
		job := &s.Jobs[i]
		state, status, message := jobState(job, b.podStates(job.UID))
		b.set(key("jobs", job), state, status, message)
	}
	for i := range s.CronJobs {
		cj := &s.CronJobs[i]
		state, status, message := b.cronJobState(cj)
		b.set(key("cronjobs", cj), state, status, message)
	}

	for i := range s.Services {
		svc := &s.Services[i]
		state, status, message := b.serviceState(svc)
		b.set(key("services", svc), state, status, message)
	}
	for i := range s.Ingresses {
		ing := &s.Ingresses[i]
		address := ""
		for _, lb := range ing.Status.LoadBalancer.Ingress {
			address = firstOf(lb.IP, lb.Hostname)
			break
		}
		b.set(key("ingresses", ing), OK, address, "")
	}
	for i := range s.PersistentVolumeClaims {
		pvc := &s.PersistentVolumeClaims[i]
		state, status, message := b.claimState(pvc)
		b.set(key("persistentvolumeclaims", pvc), state, status, message)
	}
	for i := range s.PersistentVolumes {
		pv := &s.PersistentVolumes[i]
		state := OK
		switch pv.Status.Phase {
		case corev1.VolumeFailed:
			state = Failing
		case corev1.VolumePending:
			state = Progressing
		}
		b.set(key("persistentvolumes", pv), state, string(pv.Status.Phase), pv.Status.Message)
	}
	for i := range s.HorizontalPodAutoscalers {
		hpa := &s.HorizontalPodAutoscalers[i]
		state, status, message := hpaState(hpa)
		b.set(key("horizontalpodautoscalers", hpa), state, status, message)
	}
	for i := range s.PodDisruptionBudgets {
		pdb := &s.PodDisruptionBudgets[i]
		b.set(key("poddisruptionbudgets", pdb), OK, fmt.Sprintf("%d disruptions allowed", pdb.Status.DisruptionsAllowed), "")
	}

	for i := range s.Gateways {
		gw := &s.Gateways[i]
		status, message := gw.Summary()
		b.set(key("gateways", gw), summaryState(status), status, message)
	}
	for i := range s.HTTPRoutes {
		route := &s.HTTPRoutes[i]
		status, message := route.Summary()
		b.set(key("httproutes", route), summaryState(status), status, message)
	}
	for i := range s.GatewayClasses {
		class := &s.GatewayClasses[i]
		status, message := class.Summary()
		state := summaryState(status)
		if status == "Pending" && b.now.Sub(class.CreationTimestamp.Time) > stuckAfter {
			state, message = Failing, "No controller has accepted it. Is "+class.Spec.ControllerName+" installed?"
		}
		b.set(key("gatewayclasses", class), state, status, message)
	}
}

// ---- Nodes (machines) ----

func nodeState(n *corev1.Node) (State, string, string) {
	state, status, message := Failing, "Unknown", "The node has never reported its status."
	for _, c := range n.Status.Conditions {
		if c.Type != corev1.NodeReady {
			continue
		}
		switch c.Status {
		case corev1.ConditionTrue:
			state, status, message = OK, "Ready", ""
		case corev1.ConditionFalse:
			state, status, message = Failing, "NotReady", c.Message
		default:
			state, status, message = Failing, "Unknown", c.Message
		}
	}
	if state == OK {
		for _, c := range n.Status.Conditions {
			pressure := c.Type == corev1.NodeMemoryPressure || c.Type == corev1.NodeDiskPressure ||
				c.Type == corev1.NodePIDPressure || c.Type == corev1.NodeNetworkUnavailable
			if pressure && c.Status == corev1.ConditionTrue {
				state, message = Degraded, c.Message
				status += "," + string(c.Type)
			}
		}
	}
	if n.Spec.Unschedulable {
		status += ",SchedulingDisabled" // cordoned on purpose; kubectl shows it the same way
	}
	return state, status, message
}

// ---- Pods ----

var initProgress = regexp.MustCompile(`^Init:\d+/\d+$`)

// podState turns the status kubectl shows into a state. Waiting is normal
// for a while (pulling images, starting); after stuckAfter it's failing.
func (b *builder) podState(pod *corev1.Pod) (State, string, string) {
	status := kube.PodStatus(pod)
	message := podMessage(pod)
	age := b.now.Sub(pod.CreationTimestamp.Time)

	switch {
	case status == "Running":
		ready := podCondition(pod, corev1.PodReady)
		if ready != nil && ready.Status == corev1.ConditionTrue {
			return OK, status, ""
		}
		since := pod.CreationTimestamp.Time
		if ready != nil && !ready.LastTransitionTime.IsZero() {
			since = ready.LastTransitionTime.Time
		}
		if b.now.Sub(since) < stuckAfter {
			return Progressing, status, ""
		}
		return Degraded, status, firstOf(message, "Running, but not ready.")
	case status == "Completed" || status == "Succeeded":
		return OK, status, ""
	case status == "Terminating":
		if pod.DeletionTimestamp != nil && b.now.Sub(pod.DeletionTimestamp.Time) > stuckAfter {
			return Failing, status, "It has been terminating for more than 5 minutes."
		}
		return Progressing, status, ""
	case status == "Pending" || status == "ContainerCreating" || status == "PodInitializing" || initProgress.MatchString(status):
		if age < stuckAfter {
			return Progressing, status, message
		}
		return Failing, status, firstOf(message, "It has been starting for more than 5 minutes.")
	default:
		// CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError,
		// Init:Error, OOMKilled, Error, Evicted, Unknown, ...
		return Failing, status, message
	}
}

// podMessage finds the most useful sentence about a pod: a waiting or
// terminated container's message, or the pod's own (e.g. why it was evicted,
// or why it can't be scheduled).
func podMessage(pod *corev1.Pod) string {
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, c := range statuses {
		switch {
		case c.State.Waiting != nil && c.State.Waiting.Message != "":
			return c.State.Waiting.Message
		case c.State.Terminated != nil && c.State.Terminated.ExitCode != 0 && c.State.Terminated.Message != "":
			return c.State.Terminated.Message
		}
	}
	if pod.Status.Message != "" {
		return pod.Status.Message
	}
	if c := podCondition(pod, corev1.PodScheduled); c != nil && c.Status == corev1.ConditionFalse {
		return c.Message
	}
	return ""
}

func podCondition(pod *corev1.Pod, t corev1.PodConditionType) *corev1.PodCondition {
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == t {
			return &pod.Status.Conditions[i]
		}
	}
	return nil
}

// podStates lists the states of the pods an object owns.
func (b *builder) podStates(owner types.UID) []State {
	var states []State
	for _, k := range b.s.Owned(owner) {
		if k.Resource == "pods" {
			states = append(states, b.g.nodes[k.String()].State)
		}
	}
	return states
}

// ---- Workloads ----

// replicas is what a workload says about its pods.
type replicas struct {
	desired, ready, updated int32
	rolling                 bool   // the controller hasn't caught up with a change yet
	failure                 string // a condition that says the workload is stuck
}

// setReplicas works out a workload's state from its counts, its conditions
// and its pods:
//   - none wanted, or every one ready and up to date: OK
//   - a condition says it's stuck (deadline exceeded, can't create pods): failing
//   - some pods unhealthy (failing, or not ready for too long): degraded, or
//     failing when none are ready
//   - otherwise it's still getting there: progressing
func (b *builder) setReplicas(id string, r replicas, pods []State) {
	status := fmt.Sprintf("%d/%d", r.ready, r.desired)
	unhealthy := 0
	for _, state := range pods {
		if state == Failing || state == Degraded {
			unhealthy++
		}
	}
	switch {
	case r.desired == 0:
		b.set(id, OK, status, "")
	case r.failure != "":
		b.set(id, Failing, status, r.failure)
	case r.ready >= r.desired && r.updated >= r.desired && !r.rolling:
		b.set(id, OK, status, "")
	case unhealthy > 0 && r.ready == 0:
		b.set(id, Failing, status, "None of its pods are ready.")
	case unhealthy > 0:
		b.set(id, Degraded, status, fmt.Sprintf("%d of its pods aren't healthy.", unhealthy))
	default:
		b.set(id, Progressing, status, "")
	}
}

func replicaCount(n *int32) int32 {
	if n == nil {
		return 1 // Kubernetes treats "not set" as 1
	}
	return *n
}

func rsCondition(conditions []appsv1.ReplicaSetCondition, t appsv1.ReplicaSetConditionType) *appsv1.ReplicaSetCondition {
	for i := range conditions {
		if conditions[i].Type == t && conditions[i].Status == corev1.ConditionTrue {
			return &conditions[i]
		}
	}
	return nil
}

func conditionMessage(c *appsv1.ReplicaSetCondition) string {
	if c == nil {
		return ""
	}
	return firstOf(c.Message, c.Reason)
}

func jobState(job *batchv1.Job, pods []State) (State, string, string) {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return OK, "Complete", ""
		case batchv1.JobFailed:
			return Failing, "Failed", firstOf(c.Message, c.Reason)
		case batchv1.JobSuspended:
			return OK, "Suspended", ""
		}
	}
	for _, state := range pods {
		if state == Failing {
			return Degraded, "Running", "Some of its pods are failing; it may retry."
		}
	}
	return Progressing, "Running", ""
}

// cronJobState: a CronJob is as healthy as its latest run.
func (b *builder) cronJobState(cj *batchv1.CronJob) (State, string, string) {
	if cj.Spec.Suspend != nil && *cj.Spec.Suspend {
		return OK, "Suspended", ""
	}
	var jobs []*batchv1.Job
	for _, k := range b.s.Owned(cj.UID) {
		if obj, ok := b.s.Get(k.Resource, k.Namespace, k.Name); ok && k.Resource == "jobs" {
			jobs = append(jobs, obj.(*batchv1.Job))
		}
	}
	if len(jobs) == 0 {
		return OK, "Scheduled", ""
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreationTimestamp.After(jobs[j].CreationTimestamp.Time) })
	latest := b.g.nodes[key("jobs", jobs[0])]
	if latest.State == Failing {
		return Degraded, "Last run failed", latest.Message
	}
	if len(cj.Status.Active) > 0 {
		return OK, fmt.Sprintf("%d active", len(cj.Status.Active)), ""
	}
	return OK, "Scheduled", ""
}

// ---- Services ----

// serviceState counts the ready endpoints behind a Service. EndpointSlices
// say which pods actually receive traffic; when they can't be read, the
// selected pods' readiness is the next best thing.
func (b *builder) serviceState(svc *corev1.Service) (State, string, string) {
	switch {
	case svc.Spec.Type == corev1.ServiceTypeExternalName:
		return OK, "ExternalName", ""
	case len(svc.Spec.Selector) == 0:
		return OK, "No selector", "" // endpoints are managed by something else
	}

	total, ready := 0, 0
	if b.s.Readable("endpointslices") {
		for i := range b.s.EndpointSlices {
			slice := &b.s.EndpointSlices[i]
			if slice.Namespace != svc.Namespace || slice.Labels["kubernetes.io/service-name"] != svc.Name {
				continue
			}
			for _, e := range slice.Endpoints {
				if e.Conditions.Terminating != nil && *e.Conditions.Terminating {
					continue // on its way out during a rollout; not a problem
				}
				total++
				if e.Conditions.Ready == nil || *e.Conditions.Ready {
					ready++
				}
			}
		}
	} else {
		for _, e := range b.g.Out(key("services", svc)) {
			if pod, ok := b.object(e.To).(*corev1.Pod); ok {
				total++
				if c := podCondition(pod, corev1.PodReady); c != nil && c.Status == corev1.ConditionTrue {
					ready++
				}
			}
		}
	}

	switch {
	case total == 0:
		return Failing, "No endpoints", "Its selector matches no running pods, so it has nowhere to send traffic."
	case ready == 0:
		return Failing, fmt.Sprintf("0/%d ready", total), "None of the pods it selects are ready."
	case ready < total:
		return Degraded, fmt.Sprintf("%d/%d ready", ready, total), ""
	default:
		return OK, fmt.Sprintf("%d/%d ready", ready, total), ""
	}
}

// ---- Storage ----

// claimState: Bound is fine. Pending is fine for a while, and for as long
// as it likes when its class binds only once a pod uses the claim.
func (b *builder) claimState(pvc *corev1.PersistentVolumeClaim) (State, string, string) {
	status := string(pvc.Status.Phase)
	switch pvc.Status.Phase {
	case corev1.ClaimBound:
		return OK, status, ""
	case corev1.ClaimLost:
		return Failing, status, "Its volume is gone."
	}

	id := key("persistentvolumeclaims", pvc)
	usedByPod := false
	for _, e := range b.g.In(id) {
		usedByPod = usedByPod || strings.HasPrefix(e.From, "pods/")
	}
	for _, e := range b.g.Out(id) {
		if class, ok := b.object(e.To).(*storagev1.StorageClass); ok {
			mode := class.VolumeBindingMode
			if !usedByPod && mode != nil && *mode == storagev1.VolumeBindingWaitForFirstConsumer {
				return OK, status, "It's bound once a pod uses it."
			}
		}
	}
	if b.now.Sub(pvc.CreationTimestamp.Time) < stuckAfter {
		return Progressing, status, ""
	}
	return Failing, status, "It has been waiting for a volume for more than 5 minutes."
}

// ---- Autoscaling ----

func hpaState(hpa *autoscalingv2.HorizontalPodAutoscaler) (State, string, string) {
	status := fmt.Sprintf("%d/%d", hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas)
	for _, c := range hpa.Status.Conditions {
		if (c.Type == autoscalingv2.ScalingActive || c.Type == autoscalingv2.AbleToScale) && c.Status == corev1.ConditionFalse {
			return Degraded, status, c.Message
		}
	}
	return OK, status, ""
}

// ---- Gateway API ----

// summaryState turns a Gateway API one-word status into a state.
func summaryState(status string) State {
	switch status {
	case "Programmed", "Accepted":
		return OK
	case "Pending":
		return Progressing
	default: // NotAccepted, NotProgrammed, UnresolvedRefs
		return Failing
	}
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
