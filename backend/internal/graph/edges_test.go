package graph

import (
	"slices"
	"testing"

	"opscope/internal/kube"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOwnsEdges(t *testing.T) {
	d := meta("shop", "web")
	rs := ownedBy(meta("shop", "web-7d9f"), "Deployment", d)
	cj := meta("shop", "nightly")
	job := ownedBy(meta("shop", "nightly-1"), "CronJob", cj)

	g := build(&snapshot.Snapshot{
		Deployments: []appsv1.Deployment{deployment(d, 1, 1)},
		ReplicaSets: []appsv1.ReplicaSet{replicaSet(rs, 1, 1)},
		Pods: []corev1.Pod{
			readyPod(ownedBy(meta("shop", "web-7d9f-a"), "ReplicaSet", rs), ""),
			readyPod(ownedBy(meta("shop", "nightly-1-x"), "Job", job), ""),
		},
		CronJobs: []batchv1.CronJob{{ObjectMeta: cj}},
		Jobs:     []batchv1.Job{{ObjectMeta: job}},
	})

	findEdge(t, g, "deployments/shop/web", "replicasets/shop/web-7d9f", Owns)
	findEdge(t, g, "replicasets/shop/web-7d9f", "pods/shop/web-7d9f-a", Owns)
	findEdge(t, g, "cronjobs/shop/nightly", "jobs/shop/nightly-1", Owns)
	findEdge(t, g, "jobs/shop/nightly-1", "pods/shop/nightly-1-x", Owns)
}

func TestPodEdges(t *testing.T) {
	spec := corev1.PodSpec{
		ServiceAccountName: "web",
		ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "registry"}},
		Volumes: []corev1.Volume{
			{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}},
			{Name: "certs", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "tls"}}},
			{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}},
			{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token"}},
				{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}}},
			}}}},
		},
		Containers: []corev1.Container{{
			Name: "api",
			Env: []corev1.EnvVar{
				{Name: "STRIPE_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}, Key: "key"}}},
				{Name: "STRIPE_ID", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}, Key: "id"}}},
				{Name: "PLAIN", Value: "x"},
			},
			EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}}},
		}},
	}
	g := build(&snapshot.Snapshot{
		Nodes:                  []corev1.Node{readyNode("worker-1")},
		Pods:                   []corev1.Pod{readyPod(meta("shop", "web"), "worker-1", spec)},
		ConfigMaps:             []snapshot.ConfigMap{{ObjectMeta: meta("shop", "settings")}, {ObjectMeta: meta("shop", "kube-root-ca.crt")}},
		Secrets:                []metav1.PartialObjectMetadata{{ObjectMeta: meta("shop", "tls")}, {ObjectMeta: meta("shop", "stripe")}, {ObjectMeta: meta("shop", "registry")}},
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{{ObjectMeta: meta("shop", "data")}},
		ServiceAccounts:        []corev1.ServiceAccount{{ObjectMeta: meta("shop", "web")}},
	})
	pod := "pods/shop/web"

	findEdge(t, g, pod, "nodes/worker-1", RunsOn)
	if e := findEdge(t, g, pod, "configmaps/shop/settings", Uses); !slices.Equal(e.Via, []string{"volume config", "container api: envFrom"}) {
		t.Errorf("one edge for both references, got via %v", e.Via)
	}
	if e := findEdge(t, g, pod, "secrets/shop/stripe", Uses); !slices.Equal(e.Via, []string{"container api: env STRIPE_KEY", "container api: env STRIPE_ID"}) {
		t.Errorf("via = %v", e.Via)
	}
	findEdge(t, g, pod, "secrets/shop/tls", Uses)
	findEdge(t, g, pod, "secrets/shop/registry", Uses)
	findEdge(t, g, pod, "persistentvolumeclaims/shop/data", Uses)
	findEdge(t, g, pod, "serviceaccounts/shop/web", Uses)
	noEdge(t, g, pod, "configmaps/shop/kube-root-ca.crt")
}

func TestDefaultServiceAccountIsLeftOut(t *testing.T) {
	g := build(&snapshot.Snapshot{
		Pods:            []corev1.Pod{readyPod(meta("shop", "web"), "", corev1.PodSpec{ServiceAccountName: "default"})},
		ServiceAccounts: []corev1.ServiceAccount{{ObjectMeta: meta("shop", "default")}},
	})
	noEdge(t, g, "pods/shop/web", "serviceaccounts/shop/default")
}

func TestMissingAndUnknownReferences(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}}},
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}},
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "extra"}, Optional: ptr(true)}},
	}}}}
	s := &snapshot.Snapshot{Pods: []corev1.Pod{readyPod(meta("shop", "web"), "", spec)}}
	unreadable(s, "forbidden", "secrets")
	g := build(s)

	findEdge(t, g, "pods/shop/web", "configmaps/shop/settings", Uses)
	wantState(t, g, "configmaps/shop/settings", Missing)
	findEdge(t, g, "pods/shop/web", "secrets/shop/stripe", Uses)
	wantState(t, g, "secrets/shop/stripe", Unknown) // not allowed to read Secrets: can't say it's missing

	if _, ok := g.Node("configmaps/shop/extra"); ok {
		t.Error("an optional reference to nothing shouldn't add a missing node")
	}
}

func TestOptionalEdges(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "a"}, Optional: ptr(true)}},
		{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "b"}, Optional: ptr(true)}},
	}, Env: []corev1.EnvVar{
		{Name: "B", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "b"}, Key: "k"}}},
	}}}}
	g := build(&snapshot.Snapshot{
		Pods:       []corev1.Pod{readyPod(meta("shop", "web"), "", spec)},
		ConfigMaps: []snapshot.ConfigMap{{ObjectMeta: meta("shop", "a")}, {ObjectMeta: meta("shop", "b")}},
	})
	if !findEdge(t, g, "pods/shop/web", "configmaps/shop/a", Uses).Optional {
		t.Error("a: only optional references, so the edge is optional")
	}
	if findEdge(t, g, "pods/shop/web", "configmaps/shop/b", Uses).Optional {
		t.Error("b: one required reference makes the edge required")
	}
}

func TestTemplateEdges(t *testing.T) {
	d := deployment(meta("shop", "web"), 1, 0)
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}}},
	}}}
	rs := replicaSet(ownedBy(meta("shop", "web-7d9f"), "Deployment", d.ObjectMeta), 1, 0)
	rs.Spec.Template = d.Spec.Template
	g := build(&snapshot.Snapshot{Deployments: []appsv1.Deployment{d}, ReplicaSets: []appsv1.ReplicaSet{rs}})

	// No pods exist, but the Deployment still shows what it needs.
	findEdge(t, g, "deployments/shop/web", "secrets/shop/stripe", Uses)
	noEdge(t, g, "replicasets/shop/web-7d9f", "secrets/shop/stripe") // owned: its Deployment says it already
}

func TestSelectsEdges(t *testing.T) {
	g := build(&snapshot.Snapshot{
		Pods: []corev1.Pod{
			readyPod(meta("shop", "web-1", "app", "web"), ""),
			readyPod(meta("shop", "api-1", "app", "api"), ""),
			readyPod(meta("other", "web-1", "app", "web"), ""),
		},
		Services: []corev1.Service{
			{ObjectMeta: meta("shop", "web"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}},
			{ObjectMeta: meta("shop", "external"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, Selector: map[string]string{"app": "web"}}},
		},
		PodDisruptionBudgets: []policyv1.PodDisruptionBudget{{ObjectMeta: meta("shop", "web"), Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
		}}},
	})

	findEdge(t, g, "services/shop/web", "pods/shop/web-1", Selects)
	noEdge(t, g, "services/shop/web", "pods/shop/api-1")
	noEdge(t, g, "services/shop/web", "pods/other/web-1") // selectors stay in their namespace
	noEdge(t, g, "services/shop/external", "pods/shop/web-1")
	findEdge(t, g, "poddisruptionbudgets/shop/web", "pods/shop/web-1", Selects)
}

func TestStorageEdges(t *testing.T) {
	isDefault := map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}
	g := build(&snapshot.Snapshot{
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{
			{ObjectMeta: meta("db", "data"), Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1"}},
			{ObjectMeta: meta("db", "none"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr("")}},
			{ObjectMeta: meta("db", "fast"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr("fast")}},
		},
		PersistentVolumes: []corev1.PersistentVolume{{ObjectMeta: meta("", "pv-1"), Spec: corev1.PersistentVolumeSpec{StorageClassName: "standard"}}},
		StorageClasses: []storagev1.StorageClass{
			{ObjectMeta: metav1.ObjectMeta{Name: "standard", Annotations: isDefault}},
		},
	})

	findEdge(t, g, "persistentvolumeclaims/db/data", "persistentvolumes/pv-1", Uses)
	findEdge(t, g, "persistentvolumeclaims/db/data", "storageclasses/standard", Uses) // no class named: the default
	findEdge(t, g, "persistentvolumes/pv-1", "storageclasses/standard", Uses)
	if len(g.Out("persistentvolumeclaims/db/none")) != 0 {
		t.Error(`storageClassName: "" means no class at all`)
	}
	findEdge(t, g, "persistentvolumeclaims/db/fast", "storageclasses/fast", Uses)
	wantState(t, g, "storageclasses/fast", Missing)
}

func TestIngressEdges(t *testing.T) {
	backend := func(name string) networkingv1.IngressBackend {
		return networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: name}}
	}
	ing := networkingv1.Ingress{ObjectMeta: meta("shop", "web"), Spec: networkingv1.IngressSpec{
		IngressClassName: ptr("nginx"),
		DefaultBackend:   ptr(backend("fallback")),
		TLS:              []networkingv1.IngressTLS{{SecretName: "web-tls"}},
		Rules: []networkingv1.IngressRule{{Host: "web.local", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
			Paths: []networkingv1.HTTPIngressPath{{Path: "/api", Backend: backend("api")}, {Path: "/", Backend: backend("web")}},
		}}}},
	}}
	old := networkingv1.Ingress{ObjectMeta: meta("shop", "old"), Spec: networkingv1.IngressSpec{DefaultBackend: ptr(backend("web"))}}
	old.Annotations = map[string]string{"kubernetes.io/ingress.class": "traefik"}
	plain := networkingv1.Ingress{ObjectMeta: meta("shop", "plain"), Spec: networkingv1.IngressSpec{DefaultBackend: ptr(backend("web"))}}

	g := build(&snapshot.Snapshot{
		Ingresses: []networkingv1.Ingress{ing, old, plain},
		IngressClasses: []networkingv1.IngressClass{
			{ObjectMeta: metav1.ObjectMeta{Name: "nginx", Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"}}},
		},
		Services: []corev1.Service{{ObjectMeta: meta("shop", "web")}, {ObjectMeta: meta("shop", "api")}},
	})

	findEdge(t, g, "ingresses/shop/web", "ingressclasses/nginx", Uses)
	findEdge(t, g, "ingresses/shop/web", "secrets/shop/web-tls", Uses)
	if e := findEdge(t, g, "ingresses/shop/web", "services/shop/api", RoutesTo); !slices.Equal(e.Via, []string{"web.local/api"}) {
		t.Errorf("via = %v", e.Via)
	}
	findEdge(t, g, "ingresses/shop/web", "services/shop/web", RoutesTo)
	findEdge(t, g, "ingresses/shop/web", "services/shop/fallback", RoutesTo)
	wantState(t, g, "services/shop/fallback", Missing)
	findEdge(t, g, "ingresses/shop/old", "ingressclasses/traefik", Uses) // the older annotation
	findEdge(t, g, "ingresses/shop/plain", "ingressclasses/nginx", Uses) // the default class
}

func TestGatewayEdges(t *testing.T) {
	gw := kube.Gateway{ObjectMeta: meta("infra", "edge")}
	gw.Spec.GatewayClassName = "nginx"
	gw.Spec.Listeners = []kube.GatewayListener{{Name: "https", TLS: &kube.ListenerTLS{CertificateRefs: []kube.ObjectRef{{Name: "edge-cert"}}}}}

	route := kube.HTTPRoute{ObjectMeta: meta("shop", "web")}
	route.Spec.ParentRefs = []kube.ObjectRef{{Name: "edge", Namespace: ptr("infra"), SectionName: ptr("https")}}
	route.Spec.Rules = []kube.HTTPRouteRule{
		{BackendRefs: []kube.BackendRef{{ObjectRef: kube.ObjectRef{Name: "web"}}}},
		{BackendRefs: []kube.BackendRef{
			{ObjectRef: kube.ObjectRef{Name: "users", Namespace: ptr("accounts")}},
			{ObjectRef: kube.ObjectRef{Name: "orders", Namespace: ptr("billing")}},
			{ObjectRef: kube.ObjectRef{Name: "bucket", Kind: ptr("Bucket"), Group: ptr("storage.example.com")}},
		}},
	}

	grant := kube.ReferenceGrant{ObjectMeta: meta("accounts", "allow-shop")}
	grant.Spec.From = []kube.GrantFrom{{Group: gatewayGroup, Kind: "HTTPRoute", Namespace: "shop"}}
	grant.Spec.To = []kube.GrantTo{{Kind: "Service"}}

	g := build(&snapshot.Snapshot{
		Gateways:        []kube.Gateway{gw},
		GatewayClasses:  []kube.GatewayClass{{ObjectMeta: meta("", "nginx")}},
		HTTPRoutes:      []kube.HTTPRoute{route},
		ReferenceGrants: []kube.ReferenceGrant{grant},
		Services:        []corev1.Service{{ObjectMeta: meta("shop", "web")}, {ObjectMeta: meta("accounts", "users")}, {ObjectMeta: meta("billing", "orders")}},
	})

	findEdge(t, g, "gateways/infra/edge", "gatewayclasses/nginx", Attaches)
	findEdge(t, g, "gateways/infra/edge", "secrets/infra/edge-cert", Uses)
	if e := findEdge(t, g, "httproutes/shop/web", "gateways/infra/edge", Attaches); !slices.Equal(e.Via, []string{"listener https"}) {
		t.Errorf("via = %v", e.Via)
	}
	findEdge(t, g, "httproutes/shop/web", "services/shop/web", RoutesTo)
	findEdge(t, g, "httproutes/shop/web", "services/accounts/users", RoutesTo)
	findEdge(t, g, "httproutes/shop/web", "referencegrants/accounts/allow-shop", Permits)
	findEdge(t, g, "httproutes/shop/web", "services/billing/orders", RoutesTo)
	findEdge(t, g, "httproutes/shop/web", "referencegrants/billing/(none)", Permits)
	wantState(t, g, "referencegrants/billing/(none)", Missing)
	if len(g.Out("httproutes/shop/web")) != 6 {
		t.Errorf("a backend of another kind (a Bucket) isn't followed; edges:%s", describe(g.Out("httproutes/shop/web")))
	}
}

func TestScalesEdge(t *testing.T) {
	g := build(&snapshot.Snapshot{
		Deployments: []appsv1.Deployment{deployment(meta("shop", "web"), 2, 2)},
		HorizontalPodAutoscalers: []autoscalingv2.HorizontalPodAutoscaler{{ObjectMeta: meta("shop", "web"), Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
		}}},
	})
	findEdge(t, g, "horizontalpodautoscalers/shop/web", "deployments/shop/web", Scales)
}

// A volume made by hand may name a class no StorageClass has ("manual"),
// and so may a claim bound to it: that's not a missing object.
func TestStorageClassNamesWithoutAClass(t *testing.T) {
	g := build(&snapshot.Snapshot{
		PersistentVolumes: []corev1.PersistentVolume{{ObjectMeta: meta("", "pv-1"), Spec: corev1.PersistentVolumeSpec{StorageClassName: "manual"}}},
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{{ObjectMeta: meta("db", "data"),
			Spec:   corev1.PersistentVolumeClaimSpec{StorageClassName: ptr("manual"), VolumeName: "pv-1"},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}},
	})
	if _, ok := g.Node("storageclasses/manual"); ok {
		t.Error("no missing StorageClass should be added")
	}
}
