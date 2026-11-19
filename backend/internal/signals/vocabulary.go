package signals

import "time"

// Type is one word of the vocabulary. Rules (v1.9) match on these words,
// so the vocabulary is an API: words are added, never renamed, and a word
// that's no longer used stays here, Reserved.
type Type struct {
	Name     string
	Title    string        // a few words, for lists: "Crash loop"
	Severity string        // the default; a detector may raise it, never lower it
	Wait     time.Duration // how long the condition must last before a signal opens
	About    string        // what it means, for the docs and the Issues page
	Reserved bool          // no detector raises it yet
}

// Vocabulary is every signal type, in the order the design record lists
// them (docs/design/issues.md).
var Vocabulary = []Type{
	{Name: "crash_loop", Title: "Crash loop", Severity: Critical, About: "a container keeps exiting and is restarted after a growing wait"},
	{Name: "oom_killed", Title: "Out of memory", Severity: Critical, About: "a container was killed for using more memory than its limit, within the last hour"},
	{Name: "container_error", Title: "Container failed", Severity: Critical, About: "a container exited with an error and won't be restarted"},
	{Name: "image_pull_failed", Title: "Image can't be pulled", Severity: Critical, About: "a container's image can't be pulled"},
	{Name: "config_missing", Title: "Config missing", Severity: Critical, About: "a container can't start: a Secret, ConfigMap or key it needs is missing"},
	{Name: "init_failing", Title: "Init container failing", Severity: Critical, About: "an init container fails, so the pod never starts"},
	{Name: "unschedulable", Title: "Can't be scheduled", Severity: Critical, About: "no node can run the pod"},
	{Name: "mount_failed", Title: "Volume can't mount", Severity: Critical, Wait: 2 * time.Minute, About: "a volume can't be attached or mounted"},
	{Name: "probe_failing", Title: "Probe failing", Severity: Warning, About: "a readiness or liveness probe fails"},
	{Name: "evicted", Title: "Evicted", Severity: Warning, About: "the pod was evicted from its node"},
	{Name: "replicas_unavailable", Title: "Replicas not ready", Severity: Warning, Wait: 2 * time.Minute, About: "fewer replicas are ready than wanted"},
	{Name: "rollout_stuck", Title: "Rollout stuck", Severity: Critical, About: "a rollout passed its progress deadline"},
	{Name: "job_failed", Title: "Job failed", Severity: Warning, About: "a Job failed"},
	{Name: "create_failed", Title: "Can't create pods", Severity: Critical, About: "a controller can't create its pods"},
	{Name: "selector_matches_nothing", Title: "Selector matches nothing", Severity: Warning, Wait: 2 * time.Minute, About: "a Service's selector matches no pods"},
	{Name: "no_ready_endpoints", Title: "No ready endpoints", Severity: Critical, Wait: 2 * time.Minute, About: "a Service selects pods, and none of them is ready"},
	{Name: "backend_missing", Title: "Backend missing", Severity: Critical, About: "a route sends traffic to a Service that doesn't exist"},
	{Name: "route_not_accepted", Title: "Route not accepted", Severity: Critical, About: "a route isn't accepted by its Gateway, or its references don't resolve"},
	{Name: "gateway_not_programmed", Title: "Gateway not programmed", Severity: Critical, About: "a Gateway isn't programmed by its controller"},
	{Name: "gatewayclass_not_accepted", Title: "GatewayClass not accepted", Severity: Warning, About: "a GatewayClass isn't accepted by its controller"},
	{Name: "pvc_unbound", Title: "Claim not bound", Severity: Critical, Wait: 2 * time.Minute, About: "a claim isn't bound to a volume"},
	{Name: "volume_released", Title: "Volume released", Severity: Notice, About: "a volume's claim is gone and the volume is left behind"},
	{Name: "volume_nearly_full", Title: "Volume nearly full", Severity: Warning, About: "a volume is 90% full or more"},
	{Name: "node_not_ready", Title: "Node not ready", Severity: Critical, About: "a node isn't ready"},
	{Name: "node_pressure", Title: "Node under pressure", Severity: Warning, About: "a node is short of memory, disk or process IDs"},
	{Name: "node_unschedulable", Title: "Node cordoned", Severity: Notice, About: "a node is cordoned: no new pods are scheduled on it"},
	{Name: "cert_expiring", Title: "Certificate expiring", Severity: Notice, About: "a TLS certificate expires within 30 days"},
	{Name: "reference_missing", Title: "Reference missing", Severity: Warning, About: "a workload refers to a ConfigMap, Secret or claim that doesn't exist"},
	{Name: "dependency_unreachable", Title: "Dependency unreachable", Severity: Warning, Reserved: true,
		About: "reserved: there's no reliable way yet to tell what a pod can't reach"},
}

// TypeOf finds a type by name.
func TypeOf(name string) (Type, bool) {
	return typeOf(name)
}

// typeOf finds a type by name.
func typeOf(name string) (Type, bool) {
	for _, t := range Vocabulary {
		if t.Name == name {
			return t, true
		}
	}
	return Type{}, false
}
