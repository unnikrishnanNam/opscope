// Package kube holds what several packages need to know about Kubernetes
// objects: the clients for a cluster, how to read Gateway API resources, how
// to sum up a pod's status the way kubectl does, and how to tell "this kind
// isn't installed" or "not allowed" apart from a real failure.
//
// It exists so the table rows (internal/resources) and the cluster snapshot
// (internal/snapshot) work out these things the same way.
package kube

import (
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
)

// Clients holds the ways of talking to a cluster. Code takes interfaces (not
// the concrete clients) so tests can pass client-go's fakes.
type Clients struct {
	Kube     kubernetes.Interface // typed: built-in kinds like Pods and Services
	Dynamic  dynamic.Interface    // untyped: any kind, used for Gateway API custom resources
	Metadata metadata.Interface   // metadata only: lists Secrets without their values
}

// Skipped is a kind that couldn't be read, and why:
//
//	not_installed  the cluster doesn't have it (Gateway API)
//	forbidden      this user isn't allowed to list it (often Secrets)
//	failed         anything else; Error says what
type Skipped struct {
	Resource string `json:"resource"`
	Code     string `json:"code"`
	Error    string `json:"error,omitempty"`
}

// Skip says why reading a kind failed. Only "failed" is a real problem; the
// other two are normal on many clusters.
func Skip(resource string, err error) Skipped {
	switch {
	case errors.Is(err, ErrGatewayAPINotInstalled):
		return Skipped{Resource: resource, Code: "not_installed"}
	case apierrors.IsForbidden(err):
		return Skipped{Resource: resource, Code: "forbidden"}
	default:
		return Skipped{Resource: resource, Code: "failed", Error: err.Error()}
	}
}
