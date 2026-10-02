package resources

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Secret is one row of the secret list. It holds key NAMES only, never
// values. Values are fetched one key at a time with SecretValue, and only
// when someone clicks "Reveal" in the UI.
type Secret struct {
	Meta
	Type string   `json:"type"` // e.g. Opaque, kubernetes.io/tls, helm.sh/release.v1
	Keys []string `json:"keys"`
}

func listSecrets(ctx context.Context, client kubernetes.Interface, q Query) ([]Secret, error) {
	list, err := client.CoreV1().Secrets(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Secret, 0, len(list.Items))
	for _, s := range list.Items {
		keys := []string{}
		for key := range s.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		rows = append(rows, Secret{Meta: metaOf(s.ObjectMeta), Type: string(s.Type), Keys: keys})
	}
	sortRows(rows)
	return rows, nil
}

// SecretValue is the decoded value of one key in one secret.
type SecretValue struct {
	Value string `json:"value"`
	// Base64 is true when the value isn't readable text (a keystore, an
	// image, ...). Value is then base64-encoded so it survives JSON.
	Base64 bool `json:"base64"`
}

// ErrKeyNotFound means the secret exists but has no such key.
var ErrKeyNotFound = errors.New("the secret has no such key")

// GetSecretValue reads one key of one secret. Kubernetes stores secret
// values base64-encoded, but client-go has already decoded them for us
// into raw bytes.
func GetSecretValue(ctx context.Context, client kubernetes.Interface, namespace, name, key string) (*SecretValue, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	data, ok := secret.Data[key]
	if !ok {
		return nil, ErrKeyNotFound
	}
	if utf8.Valid(data) {
		return &SecretValue{Value: string(data)}, nil
	}
	return &SecretValue{Value: base64.StdEncoding.EncodeToString(data), Base64: true}, nil
}
