package resources

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
)

// Ingress is one row of the ingress list.
type Ingress struct {
	Meta
	Class     string   `json:"class"`     // which ingress controller handles it, e.g. "nginx"
	Hosts     []string `json:"hosts"`     // host names it routes; "*" when a rule has no host
	Addresses []string `json:"addresses"` // where the controller exposes it
	TLS       bool     `json:"tls"`       // true when it serves HTTPS
}

func listIngresses(ctx context.Context, src Source, q Query) ([]Ingress, error) {
	items, err := ListOf[*networkingv1.Ingress](ctx, src, "ingresses", q)
	if err != nil {
		return nil, err
	}

	rows := make([]Ingress, 0, len(items))
	for _, ing := range items {
		row := Ingress{
			Meta:      metaOf(ing.ObjectMeta),
			Class:     ingressClass(ing),
			Hosts:     []string{},
			Addresses: []string{},
			TLS:       len(ing.Spec.TLS) > 0,
		}

		seen := map[string]bool{} // the same host can appear in several rules
		for _, rule := range ing.Spec.Rules {
			host := rule.Host
			if host == "" {
				host = "*"
			}
			if !seen[host] {
				seen[host] = true
				row.Hosts = append(row.Hosts, host)
			}
		}

		for _, lb := range ing.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				row.Addresses = append(row.Addresses, lb.IP)
			}
			if lb.Hostname != "" {
				row.Addresses = append(row.Addresses, lb.Hostname)
			}
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// ingressClass reads the class from the spec, or from the older annotation
// that some setups still use.
func ingressClass(ing *networkingv1.Ingress) string {
	if ing.Spec.IngressClassName != nil {
		return *ing.Spec.IngressClassName
	}
	return ing.Annotations["kubernetes.io/ingress.class"]
}
