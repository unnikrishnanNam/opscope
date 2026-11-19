package collector

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"maps"
	"slices"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
)

// ContentHashAnnotation holds a ConfigMap's content hash on the cached copy,
// whose values are dropped. Opscope adds it; it's never on the cluster's own
// object, and pages never show it (the detail page reads the object
// directly).
const ContentHashAnnotation = "opscope/content-hash"

// lastAppliedAnnotation is where `kubectl apply` (and Argo CD, by default)
// keeps a copy of the whole object as it was applied: for a Secret or a
// ConfigMap, its values too. Nothing reads it from the cache, and on a
// Deployment it can be a third of the object.
const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// trim is the informer transform: what the cache and the bus keep of each
// object. It changes the freshly decoded object in place, which client-go
// allows, and is idempotent, as client-go asks: an object already in the
// cache can come through again.
func trim(obj any) (any, error) {
	if _, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		return obj, nil // holds an object from the cache, trimmed already
	}
	trimMeta(obj)
	switch o := obj.(type) {
	case *corev1.Secret:
		noteCertExpiry(o)
		dropSecretValues(o)
	case *corev1.ConfigMap:
		hashConfigMap(o)
	case *appsv1.ReplicaSet:
		dropTemplate(o)
	}
	return obj, nil
}

// dropTemplate empties a ReplicaSet's pod template. A Deployment keeps ten
// old ReplicaSets by default, each about the size of the Deployment, and
// nothing reads their templates from the cache: the graph reads the
// Deployment's, which is the current one. Owner, selector, replica counts,
// status and the revision annotation stay.
func dropTemplate(rs *appsv1.ReplicaSet) {
	rs.Spec.Template = corev1.PodTemplateSpec{}
}

// trimMeta drops what every kind carries that the cache doesn't need:
//   - managedFields' fieldsV1, the list of fields each manager owns, which
//     is often larger than the rest of the object. Who changed it, how and
//     when stay: the change log needs the managers' names.
//   - kubectl apply's copy of the object (see lastAppliedAnnotation).
func trimMeta(obj any) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	fields := m.GetManagedFields()
	for i := range fields {
		fields[i].FieldsType = ""
		fields[i].FieldsV1 = nil
	}
	m.SetManagedFields(fields)
	if annotations := m.GetAnnotations(); annotations[lastAppliedAnnotation] != "" {
		delete(annotations, lastAppliedAnnotation)
		m.SetAnnotations(annotations)
	}
}

// dropSecretValues keeps a Secret's key names and nothing they hold.
// kubectl apply's annotation, which holds them too, went in trimMeta.
// CertNotAfterAnnotation holds when a TLS Secret's certificate expires, on
// the cached copy, whose values are dropped: the one thing about a
// certificate the Issues feed needs (cert_expiring), read before the values
// go. Like the ConfigMap hash, it's Opscope's and never on the cluster.
const CertNotAfterAnnotation = "opscope/cert-not-after"

// noteCertExpiry keeps when a TLS Secret's certificate expires. Nothing
// else of the Secret's data is kept (contract 11). A Secret already trimmed
// has no data left and keeps the time it has.
func noteCertExpiry(s *corev1.Secret) {
	notAfter, ok := CertNotAfter(s)
	if !ok {
		return
	}
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations[CertNotAfterAnnotation] = notAfter.UTC().Format(time.RFC3339)
}

// CertNotAfter reads when the first certificate in a kubernetes.io/tls
// Secret's tls.crt expires: from the annotation on a cached copy, or from
// the data of one read from the cluster.
func CertNotAfter(s *corev1.Secret) (time.Time, bool) {
	if s.Type != corev1.SecretTypeTLS {
		return time.Time{}, false
	}
	if v := s.Annotations[CertNotAfterAnnotation]; v != "" {
		t, err := time.Parse(time.RFC3339, v)
		return t, err == nil
	}
	block, _ := pem.Decode(s.Data[corev1.TLSCertKey])
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, false
	}
	return cert.NotAfter, true
}

func dropSecretValues(s *corev1.Secret) {
	for key := range s.Data {
		s.Data[key] = nil
	}
	s.StringData = nil
}

// hashConfigMap replaces a ConfigMap's values with one hash of them all.
// The list shows only key names, and later releases track a ConfigMap's
// content by its hash.
func hashConfigMap(cm *corev1.ConfigMap) {
	if cm.Annotations[ContentHashAnnotation] != "" && valuesDropped(cm) {
		return // trimmed already
	}

	h := sha256.New()
	// Each part is written with its length first, so no two different
	// ConfigMaps can run together into the same bytes.
	write := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s)) + ":" + s))
	}
	for _, key := range slices.Sorted(maps.Keys(cm.Data)) {
		write("data")
		write(key)
		write(cm.Data[key])
	}
	for _, key := range slices.Sorted(maps.Keys(cm.BinaryData)) {
		write("binaryData")
		write(key)
		write(string(cm.BinaryData[key]))
	}

	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	cm.Annotations[ContentHashAnnotation] = hex.EncodeToString(h.Sum(nil))
	for key := range cm.Data {
		cm.Data[key] = ""
	}
	for key := range cm.BinaryData {
		cm.BinaryData[key] = nil
	}
}

func valuesDropped(cm *corev1.ConfigMap) bool {
	for _, v := range cm.Data {
		if v != "" {
			return false
		}
	}
	for _, v := range cm.BinaryData {
		if len(v) > 0 {
			return false
		}
	}
	return true
}
