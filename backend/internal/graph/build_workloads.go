package graph

// Builders for owners, pods and the pod templates of workloads.

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	Register(Builder{Name: "owners", Refs: owners})
	Register(Builder{Name: "pods.spec", Resource: "pods", Refs: func(obj runtime.Object, r *Refs) {
		if pod, ok := obj.(*corev1.Pod); ok {
			podSpecRefs(r, pod.Spec, pod.Namespace, "spec", pod.Name)
		}
	}})
	Register(Builder{Name: "pods.node", Resource: "pods", Refs: func(obj runtime.Object, r *Refs) {
		if pod, ok := obj.(*corev1.Pod); ok {
			r.To(RunsOn, "nodes", "", pod.Spec.NodeName, "spec.nodeName", "", false)
		}
	}})

	// Workloads refer to what their pod template needs, so one scaled to
	// zero, or whose pods can't be made, still shows it.
	template := func(resource string, spec func(runtime.Object) (*corev1.PodSpec, string, string)) {
		Register(Builder{Name: resource + ".template", Resource: resource, Refs: func(obj runtime.Object, r *Refs) {
			if s, namespace, prefix := spec(obj); s != nil {
				podSpecRefs(r, *s, namespace, prefix, "")
			}
		}})
	}
	template("deployments", func(obj runtime.Object) (*corev1.PodSpec, string, string) {
		if d, ok := obj.(*appsv1.Deployment); ok {
			return &d.Spec.Template.Spec, d.Namespace, "spec.template.spec"
		}
		return nil, "", ""
	})
	template("statefulsets", func(obj runtime.Object) (*corev1.PodSpec, string, string) {
		if s, ok := obj.(*appsv1.StatefulSet); ok {
			return &s.Spec.Template.Spec, s.Namespace, "spec.template.spec"
		}
		return nil, "", ""
	})
	template("daemonsets", func(obj runtime.Object) (*corev1.PodSpec, string, string) {
		if d, ok := obj.(*appsv1.DaemonSet); ok {
			return &d.Spec.Template.Spec, d.Namespace, "spec.template.spec"
		}
		return nil, "", ""
	})
	template("jobs", func(obj runtime.Object) (*corev1.PodSpec, string, string) {
		if j, ok := obj.(*batchv1.Job); ok {
			return &j.Spec.Template.Spec, j.Namespace, "spec.template.spec"
		}
		return nil, "", ""
	})
	template("cronjobs", func(obj runtime.Object) (*corev1.PodSpec, string, string) {
		if c, ok := obj.(*batchv1.CronJob); ok {
			return &c.Spec.JobTemplate.Spec.Template.Spec, c.Namespace, "spec.jobTemplate.spec.template.spec"
		}
		return nil, "", ""
	})

	Register(Builder{Name: "statefulsets.claims", Resource: "statefulsets", Refs: statefulSetClaims})
}

// owners reads any object's owner references: an owns edge from the owner
// to the object. Static pods name their Node as owner; that one is left
// out, since runs-on already says where the pod is and the machine didn't
// make it.
func owners(obj runtime.Object, r *Refs) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	for i, o := range m.GetOwnerReferences() {
		gv, _ := schema.ParseGroupVersion(o.APIVersion)
		if gv.Group == "" && o.Kind == "Node" {
			continue
		}
		note := ""
		if o.Controller != nil && *o.Controller {
			note = "controller"
		}
		field := fmt.Sprintf("metadata.ownerReferences[%d]", i)
		r.ToKind(Owns, gv.Group, o.Kind, m.GetNamespace(), o.Name, string(o.UID), field, note, true)
	}
}

// kubeRootCA is the ConfigMap every pod's automatic token volume reads.
// Edges to it would tie every pod in a namespace to one object and say
// nothing.
const kubeRootCA = "kube-root-ca.crt"

// podSpecRefs reads what a pod, or a pod template, refers to: ConfigMaps,
// Secrets and claims in its volumes, env and envFrom, image pull Secrets,
// and its ServiceAccount unless it's "default", which nearly every pod
// runs as. prefix is the field path to the spec. podName is "" for a
// template, which has no ephemeral claims yet.
func podSpecRefs(r *Refs, spec corev1.PodSpec, namespace, prefix, podName string) {
	for i, v := range spec.Volumes {
		at := fmt.Sprintf("%s.volumes[%d]", prefix, i)
		note := "volume " + v.Name
		switch {
		case v.ConfigMap != nil:
			r.To(References, "configmaps", namespace, v.ConfigMap.Name, at+".configMap.name", note, isTrue(v.ConfigMap.Optional))
		case v.Secret != nil:
			r.To(References, "secrets", namespace, v.Secret.SecretName, at+".secret.secretName", note, isTrue(v.Secret.Optional))
		case v.PersistentVolumeClaim != nil:
			r.To(References, "persistentvolumeclaims", namespace, v.PersistentVolumeClaim.ClaimName, at+".persistentVolumeClaim.claimName", note, false)
		case v.Ephemeral != nil && podName != "":
			// A generic ephemeral volume's claim is named after the pod and the volume.
			r.To(References, "persistentvolumeclaims", namespace, podName+"-"+v.Name, at+".ephemeral", note, false)
		case v.Projected != nil:
			for j, s := range v.Projected.Sources {
				source := fmt.Sprintf("%s.projected.sources[%d]", at, j)
				if s.ConfigMap != nil && s.ConfigMap.Name != kubeRootCA {
					r.To(References, "configmaps", namespace, s.ConfigMap.Name, source+".configMap.name", note, isTrue(s.ConfigMap.Optional))
				}
				if s.Secret != nil {
					r.To(References, "secrets", namespace, s.Secret.Name, source+".secret.name", note, isTrue(s.Secret.Optional))
				}
			}
		}
	}

	containers := func(list []corev1.Container, field string) {
		for i, c := range list {
			at := fmt.Sprintf("%s.%s[%d]", prefix, field, i)
			for j, env := range c.Env {
				if env.ValueFrom == nil {
					continue
				}
				note := "env " + env.Name
				envAt := fmt.Sprintf("%s.env[%d].valueFrom", at, j)
				if ref := env.ValueFrom.ConfigMapKeyRef; ref != nil {
					r.To(References, "configmaps", namespace, ref.Name, envAt+".configMapKeyRef.name", note, isTrue(ref.Optional))
				}
				if ref := env.ValueFrom.SecretKeyRef; ref != nil {
					r.To(References, "secrets", namespace, ref.Name, envAt+".secretKeyRef.name", note, isTrue(ref.Optional))
				}
			}
			for j, from := range c.EnvFrom {
				fromAt := fmt.Sprintf("%s.envFrom[%d]", at, j)
				if ref := from.ConfigMapRef; ref != nil {
					r.To(References, "configmaps", namespace, ref.Name, fromAt+".configMapRef.name", "envFrom", isTrue(ref.Optional))
				}
				if ref := from.SecretRef; ref != nil {
					r.To(References, "secrets", namespace, ref.Name, fromAt+".secretRef.name", "envFrom", isTrue(ref.Optional))
				}
			}
		}
	}
	containers(spec.InitContainers, "initContainers")
	containers(spec.Containers, "containers")

	for i, s := range spec.ImagePullSecrets {
		r.To(References, "secrets", namespace, s.Name, fmt.Sprintf("%s.imagePullSecrets[%d].name", prefix, i), "image pull secret", false)
	}

	account, field := spec.ServiceAccountName, prefix+".serviceAccountName"
	if account == "" {
		account, field = spec.DeprecatedServiceAccount, prefix+".serviceAccount"
	}
	if account != "default" {
		r.To(References, "serviceaccounts", namespace, account, field, "service account", false)
	}
}

// statefulSetClaims refers a StatefulSet to the claims made from its
// templates, named <template>-<statefulset>-<ordinal>. They outlive the
// pods that use them, so they're found by name, not through the pods.
func statefulSetClaims(obj runtime.Object, r *Refs) {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return
	}
	for i, t := range sts.Spec.VolumeClaimTemplates {
		prefix := t.Name + "-" + sts.Name + "-"
		r.Matching(References, Match{
			Resource:  "persistentvolumeclaims",
			Namespace: sts.Namespace,
			Selects: func(name string, _ map[string]string) bool {
				ordinal, ok := strings.CutPrefix(name, prefix)
				return ok && isDigits(ordinal)
			},
		}, fmt.Sprintf("spec.volumeClaimTemplates[%d]", i), "claim template "+t.Name)
	}
}

func isTrue(b *bool) bool {
	return b != nil && *b
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
