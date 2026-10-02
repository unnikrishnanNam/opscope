package resources

// The detail view for one object. Every kind is fetched the same way, with
// the dynamic client, so the YAML and the generic parts (metadata, owners,
// conditions, events) work for anything. Kind-specific sections are added in
// detail_sections.go.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// kindInfo says where a resource type lives in the Kubernetes API.
type kindInfo struct {
	GVR        schema.GroupVersionResource
	Kind       string
	Namespaced bool
}

func core(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: resource}
}

func group(group, version, resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}

// kinds lists every resource type that has a detail view, by its URL name.
var kinds = map[string]kindInfo{
	"namespaces":     {core("namespaces"), "Namespace", false},
	"nodes":          {core("nodes"), "Node", false},
	"pods":           {core("pods"), "Pod", true},
	"configmaps":     {core("configmaps"), "ConfigMap", true},
	"secrets":        {core("secrets"), "Secret", true},
	"services":       {core("services"), "Service", true},
	"deployments":    {group("apps", "v1", "deployments"), "Deployment", true},
	"statefulsets":   {group("apps", "v1", "statefulsets"), "StatefulSet", true},
	"daemonsets":     {group("apps", "v1", "daemonsets"), "DaemonSet", true},
	"jobs":           {group("batch", "v1", "jobs"), "Job", true},
	"cronjobs":       {group("batch", "v1", "cronjobs"), "CronJob", true},
	"ingresses":      {group("networking.k8s.io", "v1", "ingresses"), "Ingress", true},
	"gateways":       {gatewayAPI("gateways"), "Gateway", true},
	"httproutes":     {gatewayAPI("httproutes"), "HTTPRoute", true},
	"gatewayclasses": {gatewayAPI("gatewayclasses"), "GatewayClass", false},
}

// resourceForKind finds the URL name for a kind, so owners like
// "Deployment/web" can link to their own detail page.
func resourceForKind(kind string) string {
	for resource, info := range kinds {
		if info.Kind == kind {
			return resource
		}
	}
	return ""
}

// ErrUnknownKind means there's no detail view for the requested resource type.
var ErrUnknownKind = errors.New("no detail view for this resource type")

// Detail is everything the detail page shows about one object. Sections that
// don't apply to a kind are left empty, and the page skips them.
type Detail struct {
	Meta
	Kind        string            `json:"kind"`
	UID         string            `json:"uid"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Owners      []Owner           `json:"owners"`
	Fields      []Field           `json:"fields"`     // short facts, e.g. "Node: kubeworker01"
	Containers  []Container       `json:"containers"` // pods and anything with a pod template
	Conditions  []Condition       `json:"conditions"` // from status.conditions, any kind
	Tables      []Table           `json:"tables"`     // kind-specific lists, e.g. a Service's ports
	Data        []DataEntry       `json:"data"`       // ConfigMap values
	SecretKeys  []string          `json:"secretKeys"` // Secret key names; values come from the reveal endpoint
	Events      []Event           `json:"events"`
	YAML        string            `json:"yaml"`
}

type Owner struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Resource string `json:"resource,omitempty"` // set when OpScope has a page for this kind
}

type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Condition struct {
	Type           string     `json:"type"`
	Status         string     `json:"status"` // True, False or Unknown
	Reason         string     `json:"reason"`
	Message        string     `json:"message"`
	LastTransition *time.Time `json:"lastTransition"`
}

// Table is a small titled table, e.g. "Ports" with a row per port.
type Table struct {
	Title   string     `json:"title"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

type DataEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`  // empty for binary data
	Binary bool   `json:"binary"` // binaryData entries aren't shown as text
	Size   int    `json:"size"`   // in bytes
}

// GetDetail fetches one object and builds its detail view. For cluster-wide
// kinds (nodes, ...) namespace is ignored.
func GetDetail(ctx context.Context, clients Clients, resource, namespace, name string) (*Detail, error) {
	info, ok := kinds[resource]
	if !ok {
		return nil, ErrUnknownKind
	}
	if !info.Namespaced {
		namespace = ""
	}

	obj, err := clients.Dynamic.Resource(info.GVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Secrets: strip the values before anything else touches the object.
	if resource == "secrets" {
		redactSecret(obj)
	}
	// managedFields is bookkeeping for server-side apply: long and rarely useful.
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")

	d := &Detail{
		Meta: Meta{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Created:   obj.GetCreationTimestamp().Time,
		},
		Kind:        info.Kind,
		UID:         string(obj.GetUID()),
		Labels:      emptyIfNil(obj.GetLabels()),
		Annotations: emptyIfNil(obj.GetAnnotations()),
		Owners:      []Owner{},
		Fields:      []Field{},
		Containers:  []Container{},
		Tables:      []Table{},
		Data:        []DataEntry{},
		SecretKeys:  []string{},
	}
	for _, o := range obj.GetOwnerReferences() {
		d.Owners = append(d.Owners, Owner{Kind: o.Kind, Name: o.Name, Resource: resourceForKind(o.Kind)})
	}

	if d.Conditions, err = conditionsOf(obj); err != nil {
		return nil, err
	}
	if err := addSections(d, resource, obj); err != nil {
		return nil, fmt.Errorf("reading %s %s: %w", info.Kind, name, err)
	}

	yamlBytes, err := yaml.Marshal(obj.Object)
	if err != nil {
		return nil, err
	}
	d.YAML = string(yamlBytes)

	if d.Events, err = eventsFor(ctx, clients, obj, info.Namespaced); err != nil {
		return nil, err
	}
	return d, nil
}

// redactSecret replaces every value in a Secret with a placeholder, and drops
// kubectl's "last-applied-configuration" annotation, which holds a full copy
// of the Secret (values included) when it was created with `kubectl apply`.
func redactSecret(obj *unstructured.Unstructured) {
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	hidden := map[string]any{}
	for key, value := range data {
		size := 0
		if s, ok := value.(string); ok {
			size = len(s) * 3 / 4 // base64 to bytes, roughly
		}
		hidden[key] = fmt.Sprintf("(hidden, about %d bytes)", size)
	}
	unstructured.SetNestedMap(obj.Object, hidden, "data")
	unstructured.RemoveNestedField(obj.Object, "stringData")

	annotations := obj.GetAnnotations()
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
	obj.SetAnnotations(annotations)
}

// conditionsOf reads status.conditions, which most kinds have in the same
// shape (type, status, reason, message, lastTransitionTime).
func conditionsOf(obj *unstructured.Unstructured) ([]Condition, error) {
	var status struct {
		Conditions []struct {
			Type               string       `json:"type"`
			Status             string       `json:"status"`
			Reason             string       `json:"reason"`
			Message            string       `json:"message"`
			LastTransitionTime *metav1.Time `json:"lastTransitionTime"`
		} `json:"conditions"`
	}
	raw, _, _ := unstructured.NestedMap(obj.Object, "status")
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &status); err != nil {
		return nil, err
	}

	conditions := []Condition{}
	for _, c := range status.Conditions {
		conditions = append(conditions, Condition{
			Type:           c.Type,
			Status:         c.Status,
			Reason:         c.Reason,
			Message:        c.Message,
			LastTransition: timePtr(c.LastTransitionTime),
		})
	}
	return conditions, nil
}

// eventsFor lists the events about one object, newest first. Events point at
// their object by UID, which can't mix up an object with an older one that
// had the same name.
func eventsFor(ctx context.Context, clients Clients, obj *unstructured.Unstructured, namespaced bool) ([]Event, error) {
	namespace := obj.GetNamespace()
	if !namespaced {
		namespace = "" // events about nodes etc. live in "default"; search everywhere
	}
	selector := fields.OneTermEqualSelector("involvedObject.uid", string(obj.GetUID())).String()

	list, err := clients.Kube.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: selector})
	if err != nil {
		return nil, err
	}
	events := []Event{}
	for _, e := range list.Items {
		events = append(events, eventRow(&e))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].LastSeen.After(events[j].LastSeen) })
	return events, nil
}

func emptyIfNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
