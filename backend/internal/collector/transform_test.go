package collector

import (
	"maps"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

func TestTrimKeepsManagersWithoutTheirFields(t *testing.T) {
	when := metav1.NewTime(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", ManagedFields: []metav1.ManagedFieldsEntry{{
		Manager:    "kubectl-client-side-apply",
		Operation:  metav1.ManagedFieldsOperationUpdate,
		APIVersion: "v1",
		Time:       &when,
		FieldsType: "FieldsV1",
		FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{}}}`)},
	}}}}
	trim(pod)

	f := pod.ManagedFields[0]
	if f.Manager != "kubectl-client-side-apply" || f.Operation != metav1.ManagedFieldsOperationUpdate || !f.Time.Equal(&when) {
		t.Errorf("manager, operation and time should stay: %+v", f)
	}
	if f.FieldsV1 != nil || f.FieldsType != "" {
		t.Errorf("fieldsV1 should be gone: %+v", f)
	}
}

func TestTrimDropsSecretValues(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Annotations: map[string]string{
			lastAppliedAnnotation: `{"data":{"password":"aHVudGVyMg=="}}`,
			"team":                "payments",
		}},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"password": []byte("hunter2"), "user": []byte("admin")},
		StringData: map[string]string{"token": "abc"},
	}
	trim(secret)

	if len(secret.Data) != 2 || secret.Data["password"] != nil || secret.Data["user"] != nil {
		t.Errorf("keys should stay without values: %v", secret.Data)
	}
	if secret.StringData != nil {
		t.Error("stringData should be gone")
	}
	if _, ok := secret.Annotations[lastAppliedAnnotation]; ok || secret.Annotations["team"] != "payments" {
		t.Errorf("only the last-applied annotation should go: %v", secret.Annotations)
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Error("the type should stay")
	}
}

func TestTrimDropsKubectlsCopyFromEveryKind(t *testing.T) {
	applied := map[string]string{lastAppliedAnnotation: `{"data":{"db-url":"postgres://app@db/shop"}}`, "team": "payments"}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app", Annotations: maps.Clone(applied)}, Data: map[string]string{"db-url": "postgres://app@db/shop"}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Annotations: maps.Clone(applied)}}
	route := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "web", "annotations": map[string]any{lastAppliedAnnotation: "{}", "team": "payments"}}}}
	for _, obj := range []metav1.Object{cm, deployment, route} {
		trim(obj)
		if got := obj.GetAnnotations(); got[lastAppliedAnnotation] != "" || got["team"] != "payments" {
			t.Errorf("%s: only the last-applied annotation should go: %v", obj.GetName(), got)
		}
	}
	if cm.Annotations[ContentHashAnnotation] == "" {
		t.Error("the ConfigMap should still get its hash")
	}

	// An object without annotations keeps none, rather than an empty map.
	plain := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web"}}
	trim(plain)
	if plain.Annotations != nil {
		t.Errorf("annotations = %v, want none", plain.Annotations)
	}
}

func configMap(data map[string]string, binary map[string][]byte) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app"}, Data: data, BinaryData: binary}
}

func hashOf(cm *corev1.ConfigMap) string {
	trim(cm)
	return cm.Annotations[ContentHashAnnotation]
}

func TestTrimHashesConfigMapContent(t *testing.T) {
	cm := configMap(map[string]string{"a": "1", "b": "2"}, map[string][]byte{"c": {0xff}})
	hash := hashOf(cm)
	if len(hash) != 64 {
		t.Fatalf("hash = %q, want 64 hex characters", hash)
	}
	if cm.Data["a"] != "" || cm.Data["b"] != "" || cm.BinaryData["c"] != nil || len(cm.Data) != 2 || len(cm.BinaryData) != 1 {
		t.Errorf("keys should stay without values: %v %v", cm.Data, cm.BinaryData)
	}

	// Trimming again (an object from the cache coming through once more)
	// keeps the hash of the real content.
	if again := hashOf(cm); again != hash {
		t.Errorf("trimming twice changed the hash: %s, then %s", hash, again)
	}

	same := hashOf(configMap(map[string]string{"b": "2", "a": "1"}, map[string][]byte{"c": {0xff}}))
	if same != hash {
		t.Error("the same content should give the same hash")
	}
	for name, other := range map[string]*corev1.ConfigMap{
		"a value changed":       configMap(map[string]string{"a": "1", "b": "3"}, map[string][]byte{"c": {0xff}}),
		"a binary value":        configMap(map[string]string{"a": "1", "b": "2"}, map[string][]byte{"c": {0xfe}}),
		"a key renamed":         configMap(map[string]string{"a": "1", "x": "2"}, map[string][]byte{"c": {0xff}}),
		"data moved to binary":  configMap(map[string]string{"a": "1"}, map[string][]byte{"b": []byte("2"), "c": {0xff}}),
		"values run together":   configMap(map[string]string{"a": "12", "b": ""}, map[string][]byte{"c": {0xff}}),
		"empty instead of none": configMap(map[string]string{"a": "1", "b": "2", "d": ""}, map[string][]byte{"c": {0xff}}),
	} {
		if hashOf(other) == hash {
			t.Errorf("%s: should change the hash", name)
		}
	}
}

func TestTrimPassesTombstonesThrough(t *testing.T) {
	tomb := cache.DeletedFinalStateUnknown{Key: "web/app", Obj: configMap(map[string]string{"a": "1"}, nil)}
	got, err := trim(tomb)
	if err != nil || got.(cache.DeletedFinalStateUnknown).Obj.(*corev1.ConfigMap).Data["a"] != "1" {
		t.Errorf("a tombstone holds a cached object and should be left alone: %v %v", got, err)
	}
}

func TestTrimDropsReplicaSetTemplates(t *testing.T) {
	yes, three := true, int32(3)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-5d8c", Namespace: "shop",
			Annotations:     map[string]string{"deployment.kubernetes.io/revision": "7"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: &yes}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &three,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "web"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx"}}},
			},
		},
		Status: appsv1.ReplicaSetStatus{Replicas: 3, ReadyReplicas: 2},
	}
	for range 2 { // idempotent
		trim(rs)
	}
	if len(rs.Spec.Template.Spec.Containers) != 0 || rs.Spec.Template.Labels != nil {
		t.Errorf("the template stayed: %+v", rs.Spec.Template)
	}
	if *rs.Spec.Replicas != 3 || rs.Spec.Selector.MatchLabels["app"] != "web" || rs.Status.ReadyReplicas != 2 ||
		rs.Annotations["deployment.kubernetes.io/revision"] != "7" || len(rs.OwnerReferences) != 1 {
		t.Errorf("trimmed too much: %+v", rs)
	}
}
