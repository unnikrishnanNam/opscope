package resources

import (
	"context"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Node is one row of the node list.
type Node struct {
	Meta
	Status      string   `json:"status"`      // Ready, NotReady or Unknown
	Schedulable bool     `json:"schedulable"` // false when cordoned
	Roles       []string `json:"roles"`       // e.g. ["control-plane"]
	Version     string   `json:"version"`     // kubelet version
	InternalIP  string   `json:"internalIP"`
	OS          string   `json:"os"`
	Arch        string   `json:"arch"`
	OSImage     string   `json:"osImage"` // e.g. "Ubuntu 22.04.5 LTS"
	CPU         int64    `json:"cpu"`     // capacity in millicores (1000 = one core)
	Memory      int64    `json:"memory"`  // capacity in bytes
}

// listNodes ignores q.Namespace: nodes aren't inside a namespace.
func listNodes(ctx context.Context, client kubernetes.Interface, q Query) ([]Node, error) {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Node, 0, len(list.Items))
	for _, n := range list.Items {
		row := Node{
			Meta:        metaOf(n.ObjectMeta),
			Status:      nodeStatus(&n),
			Schedulable: !n.Spec.Unschedulable,
			Roles:       nodeRoles(n.Labels),
			Version:     n.Status.NodeInfo.KubeletVersion,
			OS:          n.Status.NodeInfo.OperatingSystem,
			Arch:        n.Status.NodeInfo.Architecture,
			OSImage:     n.Status.NodeInfo.OSImage,
			CPU:         n.Status.Capacity.Cpu().MilliValue(),
			Memory:      n.Status.Capacity.Memory().Value(),
		}
		for _, addr := range n.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				row.InternalIP = addr.Address
			}
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// nodeStatus reads the node's "Ready" condition, which the kubelet on the
// node keeps up to date. Unknown means the kubelet stopped reporting.
func nodeStatus(node *corev1.Node) string {
	for _, c := range node.Status.Conditions {
		if c.Type != corev1.NodeReady {
			continue
		}
		switch c.Status {
		case corev1.ConditionTrue:
			return "Ready"
		case corev1.ConditionFalse:
			return "NotReady"
		}
	}
	return "Unknown"
}

// nodeRoles finds roles in labels like "node-role.kubernetes.io/control-plane".
// Kubernetes has no real "role" field; this label is the convention kubectl uses.
func nodeRoles(labels map[string]string) []string {
	roles := []string{}
	for key, value := range labels {
		if role, ok := strings.CutPrefix(key, "node-role.kubernetes.io/"); ok && role != "" {
			roles = append(roles, role)
		} else if key == "kubernetes.io/role" && value != "" {
			roles = append(roles, value)
		}
	}
	sort.Strings(roles)
	return roles
}
