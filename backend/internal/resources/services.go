package resources

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Service is one row of the service list.
type Service struct {
	Meta
	Type        string   `json:"type"`        // ClusterIP, NodePort, LoadBalancer or ExternalName
	ClusterIP   string   `json:"clusterIP"`   // "None" for headless services
	ExternalIPs []string `json:"externalIPs"` // load balancer addresses, external IPs or the external name
	Ports       []string `json:"ports"`       // like kubectl: "80/TCP", "443:30443/TCP"
}

func listServices(ctx context.Context, client kubernetes.Interface, q Query) ([]Service, error) {
	list, err := client.CoreV1().Services(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Service, 0, len(list.Items))
	for _, svc := range list.Items {
		rows = append(rows, Service{
			Meta:        metaOf(svc.ObjectMeta),
			Type:        string(svc.Spec.Type),
			ClusterIP:   svc.Spec.ClusterIP,
			ExternalIPs: serviceExternalIPs(&svc),
			Ports:       servicePorts(svc.Spec.Ports),
		})
	}
	sortRows(rows)
	return rows, nil
}

// serviceExternalIPs collects every address a service can be reached at
// from outside the cluster.
func serviceExternalIPs(svc *corev1.Service) []string {
	ips := []string{}
	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			ips = append(ips, ingress.IP)
		}
		if ingress.Hostname != "" {
			ips = append(ips, ingress.Hostname)
		}
	}
	ips = append(ips, svc.Spec.ExternalIPs...)
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		ips = append(ips, svc.Spec.ExternalName)
	}
	return ips
}

// servicePorts formats ports the way kubectl does. A NodePort is shown
// after the port: "80:30080/TCP" means port 80, also open on every node at 30080.
func servicePorts(ports []corev1.ServicePort) []string {
	result := []string{}
	for _, p := range ports {
		if p.NodePort != 0 {
			result = append(result, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
		} else {
			result = append(result, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
	}
	return result
}
