package snapshot

import (
	"bytes"
	"fmt"
	"sort"

	"opscope/internal/kube"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// FromYAML builds a snapshot from objects written as YAML: several
// documents separated by "---", or a List as `kubectl get -o yaml` writes it,
// or both. Tests use it to turn objects captured from a real cluster into a
// snapshot. Kinds a snapshot doesn't hold are ignored; Secrets lose their
// data and ConfigMaps their values, as in Load.
//
// The snapshot comes back indexed, with no time set. A caller that marks
// kinds as skipped calls BuildIndex again afterwards.
func FromYAML(data []byte) (*Snapshot, error) {
	s := &Snapshot{Skipped: []kube.Skipped{}}
	for i, doc := range bytes.Split(data, []byte("\n---")) {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(doc, &obj.Object); err != nil {
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		if obj.Object == nil {
			continue // only comments
		}
		items := []unstructured.Unstructured{obj}
		if obj.IsList() {
			list, err := obj.ToList()
			if err != nil {
				return nil, fmt.Errorf("document %d: %w", i+1, err)
			}
			items = list.Items
		}
		for _, item := range items {
			if err := s.add(&item); err != nil {
				return nil, fmt.Errorf("%s %s/%s: %w", item.GetKind(), item.GetNamespace(), item.GetName(), err)
			}
		}
	}
	slimAll(s)
	s.BuildIndex()
	return s, nil
}

// add puts one object in the list for its kind.
func (s *Snapshot) add(obj *unstructured.Unstructured) error {
	switch obj.GetKind() {
	case "Namespace":
		return into(obj, &s.Namespaces)
	case "Node":
		return into(obj, &s.Nodes)
	case "Pod":
		return into(obj, &s.Pods)
	case "ReplicaSet":
		return into(obj, &s.ReplicaSets)
	case "Deployment":
		return into(obj, &s.Deployments)
	case "StatefulSet":
		return into(obj, &s.StatefulSets)
	case "DaemonSet":
		return into(obj, &s.DaemonSets)
	case "Job":
		return into(obj, &s.Jobs)
	case "CronJob":
		return into(obj, &s.CronJobs)
	case "Service":
		return into(obj, &s.Services)
	case "EndpointSlice":
		return into(obj, &s.EndpointSlices)
	case "Ingress":
		return into(obj, &s.Ingresses)
	case "IngressClass":
		return into(obj, &s.IngressClasses)
	case "PersistentVolumeClaim":
		return into(obj, &s.PersistentVolumeClaims)
	case "PersistentVolume":
		return into(obj, &s.PersistentVolumes)
	case "StorageClass":
		return into(obj, &s.StorageClasses)
	case "ServiceAccount":
		return into(obj, &s.ServiceAccounts)
	case "ResourceQuota":
		return into(obj, &s.ResourceQuotas)
	case "HorizontalPodAutoscaler":
		return into(obj, &s.HorizontalPodAutoscalers)
	case "PodDisruptionBudget":
		return into(obj, &s.PodDisruptionBudgets)
	case "Gateway":
		return into(obj, &s.Gateways)
	case "HTTPRoute":
		return into(obj, &s.HTTPRoutes)
	case "GatewayClass":
		return into(obj, &s.GatewayClasses)
	case "ReferenceGrant":
		return into(obj, &s.ReferenceGrants)
	case "Event":
		if t, _, _ := unstructured.NestedString(obj.Object, "type"); t == corev1.EventTypeWarning {
			return into(obj, &s.Events)
		}
	case "Secret":
		var secret corev1.Secret
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &secret); err != nil {
			return err
		}
		s.Secrets = append(s.Secrets, metav1.PartialObjectMetadata{ObjectMeta: secret.ObjectMeta})
	case "ConfigMap":
		var cm corev1.ConfigMap
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &cm); err != nil {
			return err
		}
		keys := []string{}
		for key := range cm.Data {
			keys = append(keys, key)
		}
		for key := range cm.BinaryData {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		s.ConfigMaps = append(s.ConfigMaps, ConfigMap{ObjectMeta: cm.ObjectMeta, Keys: keys})
	}
	return nil
}

func into[T any](obj *unstructured.Unstructured, list *[]T) error {
	var item T
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &item); err != nil {
		return err
	}
	*list = append(*list, item)
	return nil
}

// slimAll drops what Load drops, so a snapshot from YAML looks like one read
// from a cluster.
func slimAll(s *Snapshot) {
	slim(s.Namespaces)
	slim(s.Nodes)
	slim(s.Pods)
	slim(s.ReplicaSets)
	slim(s.Deployments)
	slim(s.StatefulSets)
	slim(s.DaemonSets)
	slim(s.Jobs)
	slim(s.CronJobs)
	slim(s.Services)
	slim(s.EndpointSlices)
	slim(s.Ingresses)
	slim(s.IngressClasses)
	slim(s.ConfigMaps)
	slim(s.Secrets)
	slim(s.PersistentVolumeClaims)
	slim(s.PersistentVolumes)
	slim(s.StorageClasses)
	slim(s.ServiceAccounts)
	slim(s.ResourceQuotas)
	slim(s.HorizontalPodAutoscalers)
	slim(s.PodDisruptionBudgets)
	slim(s.Gateways)
	slim(s.HTTPRoutes)
	slim(s.GatewayClasses)
	slim(s.ReferenceGrants)
	slim(s.Events)
}
