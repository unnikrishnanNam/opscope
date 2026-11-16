package signals

import "time"

// Type is one word of the vocabulary. Rules (v1.9) match on these words,
// so the vocabulary is an API: words are added, never renamed, and a word
// that's no longer used stays here, Reserved.
type Type struct {
	Name     string
	Severity string        // the default; a detector may raise it, never lower it
	Wait     time.Duration // how long the condition must last before a signal opens
	About    string        // what it means, for the docs and the Issues page
	Reserved bool          // no detector raises it yet
}

// Vocabulary is every signal type, in the order the design record lists
// them (docs/design/issues.md).
var Vocabulary = []Type{
	{Name: "crash_loop", Severity: Critical, About: "a container keeps exiting and is restarted after a growing wait"},
	{Name: "oom_killed", Severity: Critical, About: "a container was killed for using more memory than its limit, within the last hour"},
	{Name: "container_error", Severity: Critical, About: "a container exited with an error and won't be restarted"},
	{Name: "image_pull_failed", Severity: Critical, About: "a container's image can't be pulled"},
	{Name: "config_missing", Severity: Critical, About: "a container can't start: a Secret, ConfigMap or key it needs is missing"},
	{Name: "init_failing", Severity: Critical, About: "an init container fails, so the pod never starts"},
	{Name: "unschedulable", Severity: Critical, About: "no node can run the pod"},
	{Name: "mount_failed", Severity: Critical, Wait: 2 * time.Minute, About: "a volume can't be attached or mounted"},
	{Name: "probe_failing", Severity: Warning, About: "a readiness or liveness probe fails"},
	{Name: "evicted", Severity: Warning, About: "the pod was evicted from its node"},
	{Name: "replicas_unavailable", Severity: Warning, Wait: 2 * time.Minute, About: "fewer replicas are ready than wanted"},
	{Name: "rollout_stuck", Severity: Critical, About: "a rollout passed its progress deadline"},
	{Name: "job_failed", Severity: Warning, About: "a Job failed"},
	{Name: "create_failed", Severity: Critical, About: "a controller can't create its pods"},
	{Name: "selector_matches_nothing", Severity: Warning, About: "a Service's selector matches no pods"},
	{Name: "no_ready_endpoints", Severity: Critical, About: "a Service selects pods, and none of them is ready"},
	{Name: "backend_missing", Severity: Critical, About: "a route sends traffic to a Service that doesn't exist"},
	{Name: "route_not_accepted", Severity: Critical, About: "a route isn't accepted by its Gateway, or its references don't resolve"},
	{Name: "gateway_not_programmed", Severity: Critical, About: "a Gateway isn't programmed by its controller"},
	{Name: "gatewayclass_not_accepted", Severity: Warning, About: "a GatewayClass isn't accepted by its controller"},
	{Name: "pvc_unbound", Severity: Critical, Wait: 2 * time.Minute, About: "a claim isn't bound to a volume"},
	{Name: "volume_released", Severity: Notice, About: "a volume's claim is gone and the volume is left behind"},
	{Name: "volume_nearly_full", Severity: Warning, About: "a volume is 90% full or more"},
	{Name: "node_not_ready", Severity: Critical, About: "a node isn't ready"},
	{Name: "node_pressure", Severity: Warning, About: "a node is short of memory, disk or process IDs"},
	{Name: "node_unschedulable", Severity: Notice, About: "a node is cordoned: no new pods are scheduled on it"},
	{Name: "cert_expiring", Severity: Notice, About: "a TLS certificate expires within 30 days"},
	{Name: "reference_missing", Severity: Warning, About: "a workload refers to a ConfigMap, Secret or claim that doesn't exist"},
	{Name: "dependency_unreachable", Severity: Warning, Reserved: true,
		About: "reserved: there's no reliable way yet to tell what a pod can't reach"},
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
