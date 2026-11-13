package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ServiceAccount is one row of the ServiceAccount list.
type ServiceAccount struct {
	Meta
	Secrets     int `json:"secrets"`     // Secrets it names, like kubectl's SECRETS column
	PullSecrets int `json:"pullSecrets"` // image pull Secrets its pods get
	// Automount is false when pods using it don't get its token unless they
	// ask; null when it isn't set, which means they do.
	Automount *bool `json:"automount"`
}

func listServiceAccounts(ctx context.Context, src Source, q Query) ([]ServiceAccount, error) {
	items, err := ListOf[*corev1.ServiceAccount](ctx, src, "serviceaccounts", q)
	if err != nil {
		return nil, err
	}

	rows := make([]ServiceAccount, 0, len(items))
	for _, sa := range items {
		rows = append(rows, ServiceAccount{
			Meta:        metaOf(sa.ObjectMeta),
			Secrets:     len(sa.Secrets),
			PullSecrets: len(sa.ImagePullSecrets),
			Automount:   sa.AutomountServiceAccountToken,
		})
	}
	sortRows(rows)
	return rows, nil
}

// serviceAccountSections shows whether pods get the account's token and
// the Secrets it names. What uses it comes from the graph (AddRelations).
func serviceAccountSections(d *Detail, obj *unstructured.Unstructured) error {
	var sa corev1.ServiceAccount
	if err := convert(obj, &sa); err != nil {
		return err
	}
	automount := "yes (not set)"
	if a := sa.AutomountServiceAccountToken; a != nil && *a {
		automount = "yes"
	} else if a != nil {
		automount = "no, unless a pod asks for it"
	}
	d.add("Token mounted in pods", automount)

	secrets := Table{Title: "Secrets", Columns: []string{"Secret", "Used for"}}
	for _, s := range sa.ImagePullSecrets {
		secrets.addRow(Cell{Text: s.Name, Link: secretLink(sa.Namespace, s.Name)}, text("pulling images"))
	}
	for _, s := range sa.Secrets {
		// A reference may name another namespace; Kubernetes ignores that,
		// and so does the link.
		secrets.addRow(Cell{Text: s.Name, Link: secretLink(sa.Namespace, s.Name)}, text("token (older clusters)"))
	}
	d.addTable(secrets)
	return nil
}

func secretLink(namespace, name string) *Link {
	return &Link{Resource: "secrets", Namespace: namespace, Name: name}
}
