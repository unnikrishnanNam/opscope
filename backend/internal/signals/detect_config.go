package signals

import (
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/collector"
	"opscope/internal/graph"
)

// Detectors on configuration: certificates nearing expiry, and references
// to ConfigMaps, Secrets and claims that don't exist.

const (
	certSoon   = 30 * 24 * time.Hour // info
	certUrgent = 7 * 24 * time.Hour  // warning; expired is critical
)

// referrers are the kinds whose missing references are signals: pods no
// workload owns, and the workloads (their templates), so a Deployment's
// ten pods don't make ten signals.
var referrers = []string{"pods", "deployments", "statefulsets", "daemonsets", "jobs", "cronjobs"}

// referred are the kinds a missing reference to is a signal.
var referred = []string{"configmaps", "secrets", "persistentvolumeclaims"}

func init() {
	Default.Register(Detector{
		Name:   "secrets.certificates",
		Kinds:  []string{"secrets"},
		Types:  []string{"cert_expiring"},
		Timed:  true, // time passes; the Secret doesn't change
		Detect: certificateExpiry,
	})
	var triggers []Trigger
	for _, kind := range referred {
		triggers = append(triggers, Trigger{Resource: kind, Targets: func(obj runtime.Object, c Context) []Key {
			// One appearing or going bears on what refers to it, in its
			// namespace.
			var out []Key
			for _, r := range referrers {
				out = append(out, keysOf(r, c.List(r, namespaceOf(obj)))...)
			}
			return out
		}})
	}
	Default.Register(Detector{
		Name:     "references.missing",
		Kinds:    referrers,
		Types:    []string{"reference_missing"},
		Detect:   missingReferences,
		Triggers: triggers,
	})
}

// certificateExpiry: a TLS Secret whose certificate expires within 30
// days (info), 7 days (warning), or has expired (critical). Read from the
// date the cache keeps (collector.CertNotAfterAnnotation), never the data.
func certificateExpiry(obj runtime.Object, c Context) []Found {
	s, ok := obj.(*corev1.Secret)
	if !ok || s.DeletionTimestamp != nil {
		return nil
	}
	notAfter, ok := collector.CertNotAfter(s)
	if !ok {
		return nil
	}
	left := notAfter.Sub(c.Now())
	if left > certSoon {
		return nil
	}
	f := Found{
		Type: "cert_expiring",
		Evidence: []Evidence{{Source: FromField, Field: "data[tls.crt]: notAfter", Value: notAfter.UTC().Format(time.RFC3339),
			ResourceVersion: s.ResourceVersion}},
	}
	day := notAfter.UTC().Format("2 Jan 2006")
	switch {
	case left <= 0:
		f.Severity, f.Message = Critical, "Its certificate expired on "+day
	case left <= certUrgent:
		f.Severity, f.Message = Warning, fmt.Sprintf("Its certificate expires on %s, in %s", day, daysText(left))
	default:
		f.Message = fmt.Sprintf("Its certificate expires on %s, in %s", day, daysText(left))
	}
	return []Found{f}
}

func daysText(d time.Duration) string {
	days := int64(d / (24 * time.Hour))
	if days < 1 {
		return "less than a day"
	}
	return fmt.Sprintf("%d %s", days, plural(days, "day", "days"))
}

// missingReferences: a pod or workload refers to a ConfigMap, Secret or
// claim that doesn't exist, and doesn't mark it optional. The references
// are the graph's own (graph.RefsOf), so the Topology page shows the same
// missing object. A kind that can't be read (Secrets without the Secret
// role) isn't checked: what Opscope can't see may well exist.
func missingReferences(obj runtime.Object, c Context) []Found {
	m, ok := obj.(metav1.Object)
	if !ok || m.GetDeletionTimestamp() != nil {
		return nil
	}
	resource := ""
	switch obj.(type) {
	case *corev1.Pod:
		resource = "pods"
		if metav1.GetControllerOfNoCopy(m) != nil {
			return nil // its workload says so, once
		}
	default:
		resource = kindOfWorkload(obj)
	}
	if resource == "" {
		return nil
	}
	var missing []string
	var evidence []Evidence
	for _, ref := range graph.RefsOf(resource, obj) {
		t := ref.Target
		if ref.Kind != graph.References || t == nil || ref.Optional || !slices.Contains(referred, t.Resource) ||
			strings.HasSuffix(ref.Field, ".ephemeral") || strings.HasPrefix(ref.Field, "spec.volumeClaimTemplates") ||
			!c.Readable(t.Resource) || c.Get(t.Resource, t.Namespace, t.Name) != nil {
			continue
		}
		name := t.Kind + " " + t.Name
		if !slices.Contains(missing, name) {
			missing = append(missing, name)
		}
		evidence = append(evidence, Evidence{Source: FromField, Field: ref.Field, Value: t.Name, ResourceVersion: m.GetResourceVersion()})
	}
	if missing == nil {
		return nil
	}
	return []Found{{
		Type:     "reference_missing",
		Message:  "It refers to " + strings.Join(missing, ", ") + ", which " + plural(int64(len(missing)), "doesn't", "don't") + " exist",
		Evidence: evidence,
	}}
}

// kindOfWorkload names a workload's kind by URL name; "" for anything else.
func kindOfWorkload(obj runtime.Object) string {
	switch obj.(type) {
	case *appsv1.Deployment:
		return "deployments"
	case *appsv1.StatefulSet:
		return "statefulsets"
	case *appsv1.DaemonSet:
		return "daemonsets"
	case *batchv1.Job:
		return "jobs"
	case *batchv1.CronJob:
		return "cronjobs"
	}
	return ""
}
