package graph

import (
	"fmt"
	"strconv"

	"opscope/internal/kube"
	"opscope/internal/snapshot"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// The CA bundle every pod gets in its automatic service account volume. A
// reference to it in every pod says nothing about any of them, and would
// tie every pod in a namespace to one ConfigMap.
const rootCAConfigMap = "kube-root-ca.crt"

const gatewayGroup = "gateway.networking.k8s.io"

func (b *builder) addEdges() {
	b.ownerEdges()
	b.podEdges()
	b.templateEdges()
	b.serviceEdges()
	b.storageEdges()
	b.ingressEdges()
	b.gatewayEdges()
	b.scalingEdges()
}

// ownerEdges: Deployment → ReplicaSet → Pod, CronJob → Job → Pod, and so
// on, from each object's ownerReferences. Owners of kinds the graph doesn't
// hold (an Argo Rollout, an operator's custom resource) are left out, and so
// are Nodes: a static pod (etcd, kube-apiserver) names its node as its
// owner, but it runs on that node rather than being made by it, and its
// runs-on edge says so.
func (b *builder) ownerEdges() {
	for _, n := range b.g.Nodes() {
		obj, ok := b.s.Get(n.Resource, n.Namespace, n.Name)
		if !ok {
			continue
		}
		for _, owner := range obj.GetOwnerReferences() {
			if owner.Kind == "Node" {
				continue
			}
			if ownerKey, ok := b.s.Lookup(owner.UID); ok {
				if _, isNode := b.g.nodes[ownerKey.String()]; isNode {
					b.g.addEdge(ownerKey.String(), n.ID, Owns, "", false)
				}
			}
		}
	}
}

// podEdges: each pod to its node and to everything its spec refers to.
func (b *builder) podEdges() {
	for i := range b.s.Pods {
		pod := &b.s.Pods[i]
		id := key("pods", pod)
		if pod.Spec.NodeName != "" {
			b.ref(id, snapshot.Key{Resource: "nodes", Name: pod.Spec.NodeName}, RunsOn, "", false)
		}
		b.specRefs(id, pod.Namespace, &pod.Spec)
	}
}

// templateEdges: workloads to what their pod template refers to. A
// Deployment whose pods are never created (a quota, Pod Security) still
// shows what it needs. Only top-level workloads get these: a ReplicaSet
// owned by a Deployment, or a Job started by a CronJob, would repeat their
// owner's edges.
func (b *builder) templateEdges() {
	s := b.s
	for i := range s.Deployments {
		d := &s.Deployments[i]
		b.specRefs(key("deployments", d), d.Namespace, &d.Spec.Template.Spec)
	}
	for i := range s.StatefulSets {
		ss := &s.StatefulSets[i]
		b.specRefs(key("statefulsets", ss), ss.Namespace, &ss.Spec.Template.Spec)
	}
	for i := range s.DaemonSets {
		ds := &s.DaemonSets[i]
		b.specRefs(key("daemonsets", ds), ds.Namespace, &ds.Spec.Template.Spec)
	}
	for i := range s.CronJobs {
		cj := &s.CronJobs[i]
		b.specRefs(key("cronjobs", cj), cj.Namespace, &cj.Spec.JobTemplate.Spec.Template.Spec)
	}
	for i := range s.Jobs {
		if job := &s.Jobs[i]; len(job.OwnerReferences) == 0 {
			b.specRefs(key("jobs", job), job.Namespace, &job.Spec.Template.Spec)
		}
	}
	for i := range s.ReplicaSets {
		if rs := &s.ReplicaSets[i]; len(rs.OwnerReferences) == 0 {
			b.specRefs(key("replicasets", rs), rs.Namespace, &rs.Spec.Template.Spec)
		}
	}
}

// specRefs adds a uses edge for every ConfigMap, Secret, PersistentVolumeClaim
// and ServiceAccount a pod spec refers to, saying where each reference is.
func (b *builder) specRefs(from, namespace string, spec *corev1.PodSpec) {
	in := func(resource, name string) snapshot.Key {
		return snapshot.Key{Resource: resource, Namespace: namespace, Name: name}
	}
	isOptional := func(optional *bool) bool { return optional != nil && *optional }

	for _, v := range spec.Volumes {
		via := "volume " + v.Name
		switch {
		case v.ConfigMap != nil:
			b.ref(from, in("configmaps", v.ConfigMap.Name), Uses, via, isOptional(v.ConfigMap.Optional))
		case v.Secret != nil:
			b.ref(from, in("secrets", v.Secret.SecretName), Uses, via, isOptional(v.Secret.Optional))
		case v.PersistentVolumeClaim != nil:
			b.ref(from, in("persistentvolumeclaims", v.PersistentVolumeClaim.ClaimName), Uses, via, false)
		case v.Projected != nil:
			for _, source := range v.Projected.Sources {
				switch {
				case source.ConfigMap != nil && source.ConfigMap.Name != rootCAConfigMap:
					b.ref(from, in("configmaps", source.ConfigMap.Name), Uses, via, isOptional(source.ConfigMap.Optional))
				case source.Secret != nil:
					b.ref(from, in("secrets", source.Secret.Name), Uses, via, isOptional(source.Secret.Optional))
				}
			}
		}
	}

	containers := append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...)
	for _, c := range containers {
		for _, env := range c.Env {
			if env.ValueFrom == nil {
				continue
			}
			via := fmt.Sprintf("container %s: env %s", c.Name, env.Name)
			if ref := env.ValueFrom.ConfigMapKeyRef; ref != nil {
				b.ref(from, in("configmaps", ref.Name), Uses, via, isOptional(ref.Optional))
			}
			if ref := env.ValueFrom.SecretKeyRef; ref != nil {
				b.ref(from, in("secrets", ref.Name), Uses, via, isOptional(ref.Optional))
			}
		}
		for _, envFrom := range c.EnvFrom {
			via := fmt.Sprintf("container %s: envFrom", c.Name)
			if ref := envFrom.ConfigMapRef; ref != nil {
				b.ref(from, in("configmaps", ref.Name), Uses, via, isOptional(ref.Optional))
			}
			if ref := envFrom.SecretRef; ref != nil {
				b.ref(from, in("secrets", ref.Name), Uses, via, isOptional(ref.Optional))
			}
		}
	}

	for _, secret := range spec.ImagePullSecrets {
		b.ref(from, in("secrets", secret.Name), Uses, "imagePullSecret", false)
	}
	// Every pod has a service account, almost always "default". An edge to
	// it from every pod would say nothing; one that's named is worth showing.
	if sa := spec.ServiceAccountName; sa != "" && sa != "default" {
		b.ref(from, in("serviceaccounts", sa), Uses, "serviceAccount", false)
	}
}

// serviceEdges: Services and PodDisruptionBudgets to the pods they select.
func (b *builder) serviceEdges() {
	for i := range b.s.Services {
		svc := &b.s.Services[i]
		if len(svc.Spec.Selector) == 0 || svc.Spec.Type == corev1.ServiceTypeExternalName {
			continue // endpoints are managed by hand, or it's only a DNS name
		}
		selector := labels.SelectorFromSet(svc.Spec.Selector)
		for _, pod := range b.podsByNamespace[svc.Namespace] {
			if selector.Matches(labels.Set(pod.Labels)) {
				b.g.addEdge(key("services", svc), key("pods", pod), Selects, "", false)
			}
		}
	}
	for i := range b.s.PodDisruptionBudgets {
		pdb := &b.s.PodDisruptionBudgets[i]
		if pdb.Spec.Selector == nil {
			continue // selects nothing
		}
		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			continue
		}
		for _, pod := range b.podsByNamespace[pdb.Namespace] {
			if selector.Matches(labels.Set(pod.Labels)) {
				b.g.addEdge(key("poddisruptionbudgets", pdb), key("pods", pod), Selects, "", false)
			}
		}
	}
}

// storageEdges: claims to their volumes and storage classes.
func (b *builder) storageEdges() {
	defaultClass := ""
	for i := range b.s.StorageClasses {
		if sc := &b.s.StorageClasses[i]; sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			defaultClass = sc.Name
		}
	}

	for i := range b.s.PersistentVolumeClaims {
		pvc := &b.s.PersistentVolumeClaims[i]
		id := key("persistentvolumeclaims", pvc)
		if pvc.Spec.VolumeName != "" {
			b.ref(id, snapshot.Key{Resource: "persistentvolumes", Name: pvc.Spec.VolumeName}, Uses, "volumeName", false)
		}
		// No class named means the default class; an empty name means none.
		class := defaultClass
		if pvc.Spec.StorageClassName != nil {
			class = *pvc.Spec.StorageClassName
		}
		if class != "" {
			b.ref(id, snapshot.Key{Resource: "storageclasses", Name: class}, Uses, "storageClassName", false)
		}
	}
	for i := range b.s.PersistentVolumes {
		pv := &b.s.PersistentVolumes[i]
		if pv.Spec.StorageClassName != "" {
			b.ref(key("persistentvolumes", pv), snapshot.Key{Resource: "storageclasses", Name: pv.Spec.StorageClassName}, Uses, "storageClassName", false)
		}
	}
}

// ingressEdges: Ingresses to their class, their TLS Secrets and the
// Services they send traffic to.
func (b *builder) ingressEdges() {
	defaultClass := ""
	for i := range b.s.IngressClasses {
		if ic := &b.s.IngressClasses[i]; ic.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true" {
			defaultClass = ic.Name
		}
	}

	for i := range b.s.Ingresses {
		ing := &b.s.Ingresses[i]
		id := key("ingresses", ing)
		in := func(resource, name string) snapshot.Key {
			return snapshot.Key{Resource: resource, Namespace: ing.Namespace, Name: name}
		}

		// The class can also be named by an older annotation.
		class := ing.Annotations["kubernetes.io/ingress.class"]
		if ing.Spec.IngressClassName != nil {
			class = *ing.Spec.IngressClassName
		}
		if class == "" {
			class = defaultClass
		}
		if class != "" {
			b.ref(id, snapshot.Key{Resource: "ingressclasses", Name: class}, Uses, "ingressClassName", false)
		}

		for _, tls := range ing.Spec.TLS {
			if tls.SecretName != "" {
				b.ref(id, in("secrets", tls.SecretName), Uses, "tls", false)
			}
		}
		if backend := ing.Spec.DefaultBackend; backend != nil && backend.Service != nil {
			b.ref(id, in("services", backend.Service.Name), RoutesTo, "default backend", false)
		}
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil {
					b.ref(id, in("services", path.Backend.Service.Name), RoutesTo, orStar(rule.Host)+path.Path, false)
				}
			}
		}
	}
}

// gatewayEdges: Gateways to their class and certificates, and HTTPRoutes to
// their Gateways and backend Services. A backend in another namespace needs
// a ReferenceGrant there; the route gets a permits edge to the grant, or to
// a missing one when there's none.
func (b *builder) gatewayEdges() {
	for i := range b.s.Gateways {
		gw := &b.s.Gateways[i]
		id := key("gateways", gw)
		b.ref(id, snapshot.Key{Resource: "gatewayclasses", Name: gw.Spec.GatewayClassName}, Attaches, "gatewayClassName", false)
		for _, listener := range gw.Spec.Listeners {
			if listener.TLS == nil {
				continue
			}
			for _, cert := range listener.TLS.CertificateRefs {
				if isKind(cert, "", "Secret") {
					b.ref(id, snapshot.Key{Resource: "secrets", Namespace: refNamespace(cert, gw.Namespace), Name: cert.Name}, Uses, "listener "+listener.Name, false)
				}
			}
		}
	}

	for i := range b.s.HTTPRoutes {
		route := &b.s.HTTPRoutes[i]
		id := key("httproutes", route)
		for _, parent := range route.Spec.ParentRefs {
			if !isKind(parent, gatewayGroup, "Gateway") {
				continue
			}
			via := ""
			if parent.SectionName != nil {
				via = "listener " + *parent.SectionName
			}
			b.ref(id, snapshot.Key{Resource: "gateways", Namespace: refNamespace(parent, route.Namespace), Name: parent.Name}, Attaches, via, false)
		}
		for r, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				if !isKind(backend.ObjectRef, "", "Service") {
					continue
				}
				namespace := refNamespace(backend.ObjectRef, route.Namespace)
				b.ref(id, snapshot.Key{Resource: "services", Namespace: namespace, Name: backend.Name}, RoutesTo, "rule "+strconv.Itoa(r+1), false)
				if namespace != route.Namespace {
					b.ref(id, b.grantFor(route.Namespace, namespace, backend.Name), Permits, "rule "+strconv.Itoa(r+1), false)
				}
			}
		}
	}
}

// noGrant names the missing ReferenceGrant a cross-namespace reference
// needs. Parentheses can't appear in Kubernetes names, so it can never
// clash with a real grant.
const noGrant = "(none)"

// grantFor finds the ReferenceGrant in toNamespace that lets HTTPRoutes in
// fromNamespace reach the Service service, or names a missing one.
func (b *builder) grantFor(fromNamespace, toNamespace, service string) snapshot.Key {
	for i := range b.s.ReferenceGrants {
		grant := &b.s.ReferenceGrants[i]
		if grant.Namespace != toNamespace {
			continue
		}
		fromOK, toOK := false, false
		for _, from := range grant.Spec.From {
			fromOK = fromOK || (from.Group == gatewayGroup && from.Kind == "HTTPRoute" && from.Namespace == fromNamespace)
		}
		for _, to := range grant.Spec.To {
			toOK = toOK || (to.Group == "" && to.Kind == "Service" && (to.Name == nil || *to.Name == service))
		}
		if fromOK && toOK {
			return snapshot.Key{Resource: "referencegrants", Namespace: toNamespace, Name: grant.Name}
		}
	}
	return snapshot.Key{Resource: "referencegrants", Namespace: toNamespace, Name: noGrant}
}

// scalingEdges: HorizontalPodAutoscalers to what they scale.
func (b *builder) scalingEdges() {
	for i := range b.s.HorizontalPodAutoscalers {
		hpa := &b.s.HorizontalPodAutoscalers[i]
		target := hpa.Spec.ScaleTargetRef
		if resource, ok := resourceOf[target.Kind]; ok {
			b.ref(key("horizontalpodautoscalers", hpa), snapshot.Key{Resource: resource, Namespace: hpa.Namespace, Name: target.Name}, Scales, "", false)
		}
	}
}

// isKind says whether a Gateway API reference points at the given group and
// kind. Unset fields mean the usual ones for that field, which callers pass.
func isKind(ref kube.ObjectRef, group, kind string) bool {
	refGroup, refKind := group, kind
	if ref.Group != nil {
		refGroup = *ref.Group
	}
	if ref.Kind != nil {
		refKind = *ref.Kind
	}
	return refGroup == group && refKind == kind
}

func refNamespace(ref kube.ObjectRef, own string) string {
	if ref.Namespace != nil && *ref.Namespace != "" {
		return *ref.Namespace
	}
	return own
}

func orStar(host string) string {
	if host == "" {
		return "*"
	}
	return host
}
