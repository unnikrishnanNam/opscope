package resources

// Kind-specific parts of the detail view. Each function converts the raw
// object into its typed struct (like corev1.Pod) and picks out what's worth
// showing: short facts (Fields), containers, and small tables.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Container is one container of a pod, or of a workload's pod template.
// Status fields (Ready, Restarts, State, ...) are only filled in for pods.
type Container struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Role     string   `json:"role"` // "init", "sidecar" or "" for a normal container
	Ports    []string `json:"ports"`
	Requests string   `json:"requests"` // e.g. "cpu 100m, memory 128Mi"
	Limits   string   `json:"limits"`

	Ready       *bool  `json:"ready"` // null when we don't know (pod templates)
	Restarts    int32  `json:"restarts"`
	State       string `json:"state"`       // Running, Waiting or Terminated
	StateReason string `json:"stateReason"` // e.g. CrashLoopBackOff, "Completed (exit 0)"
	LastExit    string `json:"lastExit"`    // why it last stopped, e.g. "OOMKilled (exit 137)"
}

// addSections fills in the parts of d that depend on the kind.
func addSections(d *Detail, resource string, obj *unstructured.Unstructured) error {
	switch resource {
	case "pods":
		return podSections(d, obj)
	case "deployments":
		return deploymentSections(d, obj)
	case "statefulsets":
		return statefulSetSections(d, obj)
	case "daemonsets":
		return daemonSetSections(d, obj)
	case "jobs":
		return jobSections(d, obj)
	case "cronjobs":
		return cronJobSections(d, obj)
	case "services":
		return serviceSections(d, obj)
	case "ingresses":
		return ingressSections(d, obj)
	case "configmaps":
		return configMapSections(d, obj)
	case "secrets":
		return secretSections(d, obj)
	case "nodes":
		return nodeSections(d, obj)
	case "namespaces":
		phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
		d.add("Status", phase)
	case "gateways":
		return gatewaySections(d, obj)
	case "httproutes":
		return httpRouteSections(d, obj)
	case "gatewayclasses":
		return gatewayClassSections(d, obj)
	}
	return nil
}

// convert copies a raw object into a typed struct, like json.Unmarshal.
func convert(obj *unstructured.Unstructured, into any) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, into)
}

// add appends a short fact, skipping empty values so the page stays tidy.
func (d *Detail) add(label, value string) {
	if value != "" {
		d.Fields = append(d.Fields, Field{Label: label, Value: value})
	}
}

// ---- Pods and pod templates ----

func podSections(d *Detail, obj *unstructured.Unstructured) error {
	var pod corev1.Pod
	if err := convert(obj, &pod); err != nil {
		return err
	}
	d.add("Status", podStatus(&pod))
	d.add("Node", pod.Spec.NodeName)
	d.add("Pod IP", pod.Status.PodIP)
	d.add("Service account", pod.Spec.ServiceAccountName)
	d.add("QoS class", string(pod.Status.QOSClass))
	d.add("Restart policy", string(pod.Spec.RestartPolicy))

	// Look up each container's live status by name.
	statuses := map[string]corev1.ContainerStatus{}
	for _, s := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		statuses[s.Name] = s
	}
	for _, c := range pod.Spec.InitContainers {
		role := "init"
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			role = "sidecar"
		}
		d.Containers = append(d.Containers, withStatus(container(c, role), statuses[c.Name]))
	}
	for _, c := range pod.Spec.Containers {
		d.Containers = append(d.Containers, withStatus(container(c, ""), statuses[c.Name]))
	}
	return nil
}

// templateContainers lists the containers of a pod template (used by
// Deployments, StatefulSets, DaemonSets, Jobs and CronJobs).
func templateContainers(spec corev1.PodSpec) []Container {
	containers := []Container{}
	for _, c := range spec.InitContainers {
		containers = append(containers, container(c, "init"))
	}
	for _, c := range spec.Containers {
		containers = append(containers, container(c, ""))
	}
	return containers
}

func container(c corev1.Container, role string) Container {
	result := Container{
		Name:     c.Name,
		Image:    c.Image,
		Role:     role,
		Ports:    []string{},
		Requests: resourceList(c.Resources.Requests),
		Limits:   resourceList(c.Resources.Limits),
	}
	for _, p := range c.Ports {
		port := fmt.Sprintf("%d/%s", p.ContainerPort, p.Protocol)
		if p.Name != "" {
			port += " (" + p.Name + ")"
		}
		result.Ports = append(result.Ports, port)
	}
	return result
}

func withStatus(c Container, s corev1.ContainerStatus) Container {
	if s.Name == "" {
		return c // no status yet, e.g. the pod hasn't been scheduled
	}
	ready := s.Ready
	c.Ready = &ready
	c.Restarts = s.RestartCount
	switch {
	case s.State.Running != nil:
		c.State = "Running"
	case s.State.Waiting != nil:
		c.State, c.StateReason = "Waiting", s.State.Waiting.Reason
	case s.State.Terminated != nil:
		c.State, c.StateReason = "Terminated", exitText(s.State.Terminated)
	}
	if t := s.LastTerminationState.Terminated; t != nil {
		c.LastExit = exitText(t)
	}
	return c
}

func exitText(t *corev1.ContainerStateTerminated) string {
	return fmt.Sprintf("%s (exit %d)", orDefault(t.Reason, "Exited"), t.ExitCode)
}

// resourceList formats requests or limits: "cpu 100m, memory 128Mi".
func resourceList(list corev1.ResourceList) string {
	parts := []string{}
	for name, quantity := range list {
		parts = append(parts, string(name)+" "+quantity.String())
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// labelText formats a label selector or node selector: "app=web, tier=api".
func labelText(labels map[string]string) string {
	parts := []string{}
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func selectorText(s *metav1.LabelSelector) string {
	if s == nil {
		return ""
	}
	return metav1.FormatLabelSelector(s)
}

// ---- Workloads ----

func deploymentSections(d *Detail, obj *unstructured.Unstructured) error {
	var dep appsv1.Deployment
	if err := convert(obj, &dep); err != nil {
		return err
	}
	d.add("Replicas", fmt.Sprintf("%d desired, %d ready, %d up to date, %d available",
		replicas(dep.Spec.Replicas), dep.Status.ReadyReplicas, dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas))
	d.add("Strategy", string(dep.Spec.Strategy.Type))
	d.add("Selector", selectorText(dep.Spec.Selector))
	d.Containers = templateContainers(dep.Spec.Template.Spec)
	return nil
}

func statefulSetSections(d *Detail, obj *unstructured.Unstructured) error {
	var sts appsv1.StatefulSet
	if err := convert(obj, &sts); err != nil {
		return err
	}
	d.add("Replicas", fmt.Sprintf("%d desired, %d ready", replicas(sts.Spec.Replicas), sts.Status.ReadyReplicas))
	d.add("Service", sts.Spec.ServiceName)
	d.add("Update strategy", string(sts.Spec.UpdateStrategy.Type))
	d.add("Selector", selectorText(sts.Spec.Selector))
	d.Containers = templateContainers(sts.Spec.Template.Spec)
	return nil
}

func daemonSetSections(d *Detail, obj *unstructured.Unstructured) error {
	var ds appsv1.DaemonSet
	if err := convert(obj, &ds); err != nil {
		return err
	}
	d.add("Nodes", fmt.Sprintf("%d desired, %d current, %d ready",
		ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled, ds.Status.NumberReady))
	d.add("Node selector", labelText(ds.Spec.Template.Spec.NodeSelector))
	d.add("Update strategy", string(ds.Spec.UpdateStrategy.Type))
	d.add("Selector", selectorText(ds.Spec.Selector))
	d.Containers = templateContainers(ds.Spec.Template.Spec)
	return nil
}

func jobSections(d *Detail, obj *unstructured.Unstructured) error {
	var job batchv1.Job
	if err := convert(obj, &job); err != nil {
		return err
	}
	status, _ := jobStatus(&job)
	d.add("Status", status)
	d.add("Pods", fmt.Sprintf("%d active, %d succeeded, %d failed", job.Status.Active, job.Status.Succeeded, job.Status.Failed))
	if job.Spec.Completions != nil {
		d.add("Completions needed", strconv.Itoa(int(*job.Spec.Completions)))
	}
	if job.Spec.Parallelism != nil {
		d.add("Parallelism", strconv.Itoa(int(*job.Spec.Parallelism)))
	}
	if job.Spec.BackoffLimit != nil {
		d.add("Retries allowed", strconv.Itoa(int(*job.Spec.BackoffLimit)))
	}
	d.Containers = templateContainers(job.Spec.Template.Spec)
	return nil
}

func cronJobSections(d *Detail, obj *unstructured.Unstructured) error {
	var cj batchv1.CronJob
	if err := convert(obj, &cj); err != nil {
		return err
	}
	d.add("Schedule", cj.Spec.Schedule)
	if cj.Spec.TimeZone != nil {
		d.add("Time zone", *cj.Spec.TimeZone)
	}
	d.add("Suspended", strconv.FormatBool(cj.Spec.Suspend != nil && *cj.Spec.Suspend))
	d.add("Concurrency", string(cj.Spec.ConcurrencyPolicy))
	d.add("Active jobs", strconv.Itoa(len(cj.Status.Active)))
	d.Containers = templateContainers(cj.Spec.JobTemplate.Spec.Template.Spec)
	return nil
}

// ---- Networking ----

func serviceSections(d *Detail, obj *unstructured.Unstructured) error {
	var svc corev1.Service
	if err := convert(obj, &svc); err != nil {
		return err
	}
	d.add("Type", string(svc.Spec.Type))
	d.add("Cluster IP", svc.Spec.ClusterIP)
	d.add("External", strings.Join(serviceExternalIPs(&svc), ", "))
	d.add("Selector", labelText(svc.Spec.Selector))
	d.add("Session affinity", string(svc.Spec.SessionAffinity))

	ports := Table{Title: "Ports", Columns: []string{"Name", "Port", "Target port", "Node port", "Protocol"}}
	for _, p := range svc.Spec.Ports {
		nodePort := ""
		if p.NodePort != 0 {
			nodePort = strconv.Itoa(int(p.NodePort))
		}
		ports.Rows = append(ports.Rows, []string{p.Name, strconv.Itoa(int(p.Port)), p.TargetPort.String(), nodePort, string(p.Protocol)})
	}
	d.addTable(ports)
	return nil
}

func ingressSections(d *Detail, obj *unstructured.Unstructured) error {
	var ing networkingv1.Ingress
	if err := convert(obj, &ing); err != nil {
		return err
	}
	d.add("Class", ingressClass(&ing))
	for _, tls := range ing.Spec.TLS {
		d.add("TLS", strings.Join(tls.Hosts, ", ")+" (secret "+tls.SecretName+")")
	}

	rules := Table{Title: "Rules", Columns: []string{"Host", "Path", "Match", "Backend"}}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, p := range rule.HTTP.Paths {
			match := ""
			if p.PathType != nil {
				match = string(*p.PathType)
			}
			rules.Rows = append(rules.Rows, []string{orDefault(rule.Host, "*"), p.Path, match, ingressBackend(p.Backend)})
		}
	}
	d.addTable(rules)
	return nil
}

func ingressBackend(b networkingv1.IngressBackend) string {
	if b.Service == nil {
		return "(custom resource)"
	}
	if b.Service.Port.Name != "" {
		return b.Service.Name + ":" + b.Service.Port.Name
	}
	return fmt.Sprintf("%s:%d", b.Service.Name, b.Service.Port.Number)
}

// addTable adds a table only if it has rows.
func (d *Detail) addTable(t Table) {
	if len(t.Rows) > 0 {
		d.Tables = append(d.Tables, t)
	}
}

// ---- Config ----

func configMapSections(d *Detail, obj *unstructured.Unstructured) error {
	var cm corev1.ConfigMap
	if err := convert(obj, &cm); err != nil {
		return err
	}
	for key, value := range cm.Data {
		d.Data = append(d.Data, DataEntry{Key: key, Value: value, Size: len(value)})
	}
	for key, value := range cm.BinaryData {
		d.Data = append(d.Data, DataEntry{Key: key, Binary: true, Size: len(value)})
	}
	sort.Slice(d.Data, func(i, j int) bool { return d.Data[i].Key < d.Data[j].Key })
	return nil
}

func secretSections(d *Detail, obj *unstructured.Unstructured) error {
	secretType, _, _ := unstructured.NestedString(obj.Object, "type")
	d.add("Type", secretType)
	// The values were already replaced by redactSecret; only the key names are used here.
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	for key := range data {
		d.SecretKeys = append(d.SecretKeys, key)
	}
	sort.Strings(d.SecretKeys)
	return nil
}

// ---- Nodes ----

func nodeSections(d *Detail, obj *unstructured.Unstructured) error {
	var node corev1.Node
	if err := convert(obj, &node); err != nil {
		return err
	}
	info := node.Status.NodeInfo
	d.add("Status", nodeStatus(&node))
	d.add("Roles", strings.Join(nodeRoles(node.Labels), ", "))
	d.add("Schedulable", strconv.FormatBool(!node.Spec.Unschedulable))
	for _, a := range node.Status.Addresses {
		d.add(string(a.Type), a.Address)
	}
	d.add("OS", info.OSImage+" ("+info.OperatingSystem+"/"+info.Architecture+")")
	d.add("Kernel", info.KernelVersion)
	d.add("Container runtime", info.ContainerRuntimeVersion)
	d.add("Kubelet", info.KubeletVersion)
	d.add("Pod CIDR", node.Spec.PodCIDR)

	// Capacity is what the machine has; allocatable is what's left for pods
	// after the system reserves its share.
	capacity := Table{Title: "Resources", Columns: []string{"Resource", "Capacity", "Allocatable"}}
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourcePods, corev1.ResourceEphemeralStorage} {
		total, ok := node.Status.Capacity[name]
		if !ok {
			continue
		}
		alloc := node.Status.Allocatable[name]
		capacity.Rows = append(capacity.Rows, []string{string(name), quantityText(name, total), quantityText(name, alloc)})
	}
	d.addTable(capacity)

	taints := Table{Title: "Taints", Columns: []string{"Key", "Value", "Effect"}}
	for _, t := range node.Spec.Taints {
		taints.Rows = append(taints.Rows, []string{t.Key, t.Value, string(t.Effect)})
	}
	d.addTable(taints)
	return nil
}

// quantityText makes memory and disk sizes readable ("2.8 GiB" instead of
// "2938092Ki"); CPU and pod counts are already readable as they are.
func quantityText(name corev1.ResourceName, q resource.Quantity) string {
	if name != corev1.ResourceMemory && name != corev1.ResourceEphemeralStorage {
		return q.String()
	}
	size := float64(q.Value())
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for size >= 1024 && i < len(units)-1 {
		size /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", size, units[i])
}

// ---- Gateway API ----

func gatewaySections(d *Detail, obj *unstructured.Unstructured) error {
	var g gatewayObject
	if err := convert(obj, &g); err != nil {
		return err
	}
	status, _ := gatewayStatus(g.Status.Conditions)
	d.add("Status", status)
	d.add("Class", g.Spec.GatewayClassName)
	addresses := []string{}
	for _, a := range g.Status.Addresses {
		addresses = append(addresses, a.Value)
	}
	d.add("Addresses", strings.Join(addresses, ", "))

	// Attached routes are reported per listener in the status, by name.
	attached := map[string]int32{}
	for _, l := range g.Status.Listeners {
		attached[l.Name] = l.AttachedRoutes
	}
	listeners := Table{Title: "Listeners", Columns: []string{"Name", "Port", "Protocol", "Hostname", "Attached routes"}}
	for _, l := range g.Spec.Listeners {
		listeners.Rows = append(listeners.Rows, []string{
			l.Name, strconv.Itoa(int(l.Port)), l.Protocol, orDefault(l.Hostname, "*"), strconv.Itoa(int(attached[l.Name])),
		})
	}
	d.addTable(listeners)
	return nil
}

// httpRouteDetail reads the parts of an HTTPRoute the detail view needs on
// top of what the list uses: each rule's matches and filters, and the
// per-Gateway status.
type httpRouteDetail struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		Hostnames  []string    `json:"hostnames"`
		ParentRefs []objectRef `json:"parentRefs"`
		Rules      []struct {
			Matches []struct {
				Path *struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"path"`
				Method  string `json:"method"`
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"matches"`
			Filters []struct {
				Type string `json:"type"`
			} `json:"filters"`
			BackendRefs []struct {
				objectRef `json:",inline"`
				Weight    *int32 `json:"weight"`
			} `json:"backendRefs"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		Parents []struct {
			ParentRef      objectRef          `json:"parentRef"`
			ControllerName string             `json:"controllerName"`
			Conditions     []metav1.Condition `json:"conditions"`
		} `json:"parents"`
	} `json:"status"`
}

func httpRouteSections(d *Detail, obj *unstructured.Unstructured) error {
	var r httpRouteDetail
	if err := convert(obj, &r); err != nil {
		return err
	}
	ns := r.Metadata.Namespace
	d.add("Hostnames", strings.Join(r.Spec.Hostnames, ", "))

	rules := Table{Title: "Rules", Columns: []string{"#", "Matches", "Filters", "Backends"}}
	for i, rule := range r.Spec.Rules {
		matches := []string{}
		for _, m := range rule.Matches {
			parts := []string{}
			if m.Method != "" {
				parts = append(parts, m.Method)
			}
			if m.Path != nil {
				parts = append(parts, m.Path.Value+" ("+m.Path.Type+")")
			}
			for _, h := range m.Headers {
				parts = append(parts, h.Name+": "+h.Value)
			}
			matches = append(matches, strings.Join(parts, " "))
		}
		if len(matches) == 0 {
			matches = append(matches, "everything") // a rule without matches catches all requests
		}

		filters := []string{}
		for _, f := range rule.Filters {
			filters = append(filters, f.Type)
		}

		backends := []string{}
		for _, b := range rule.BackendRefs {
			backend := refName(b.objectRef, ns, "Service")
			if b.Port != nil {
				backend += fmt.Sprintf(":%d", *b.Port)
			}
			if b.Weight != nil && len(rule.BackendRefs) > 1 {
				backend += fmt.Sprintf(" (weight %d)", *b.Weight)
			}
			backends = append(backends, backend)
		}
		rules.Rows = append(rules.Rows, []string{
			strconv.Itoa(i + 1), strings.Join(matches, "; "), strings.Join(filters, ", "), strings.Join(backends, ", "),
		})
	}
	d.addTable(rules)

	// Routes have no conditions of their own: each Gateway reports on the
	// route separately, so this table has a row per Gateway.
	parents := Table{Title: "Status by gateway", Columns: []string{"Gateway", "Accepted", "References resolved", "Message"}}
	for _, p := range r.Status.Parents {
		accepted := apimeta.FindStatusCondition(p.Conditions, "Accepted")
		resolved := apimeta.FindStatusCondition(p.Conditions, "ResolvedRefs")
		message := ""
		for _, c := range []*metav1.Condition{accepted, resolved} {
			if c != nil && c.Status != metav1.ConditionTrue {
				message = c.Message
			}
		}
		parents.Rows = append(parents.Rows, []string{
			refName(p.ParentRef, ns, "Gateway"), conditionText(accepted), conditionText(resolved), message,
		})
	}
	d.addTable(parents)
	return nil
}

func conditionText(c *metav1.Condition) string {
	if c == nil {
		return "Unknown"
	}
	return string(c.Status)
}

func gatewayClassSections(d *Detail, obj *unstructured.Unstructured) error {
	var c gatewayClassObject
	if err := convert(obj, &c); err != nil {
		return err
	}
	d.add("Controller", c.Spec.ControllerName)
	d.add("Description", strings.TrimSpace(c.Spec.Description))
	return nil
}
