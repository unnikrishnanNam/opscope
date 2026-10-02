package clusters

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ConnectionError means the cluster config is fine but talking to the
// cluster failed. Its message is the friendly explanation; Unwrap gives the
// original error for the details.
type ConnectionError struct {
	Server string
	Err    error
}

func (e *ConnectionError) Error() string { return Explain(e.Err, e.Server) }
func (e *ConnectionError) Unwrap() error { return e.Err }

// Explain turns a low-level error from talking to a cluster into a sentence
// a person can act on. The original error is still shown as a detail.
func Explain(err error, server string) string {
	var hostErr x509.HostnameError
	var authorityErr x509.UnknownAuthorityError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error

	switch {
	case errors.As(err, &hostErr):
		return "The API server's certificate doesn't list the address " + server + ". " +
			"Use an address it does list, or set tls-server-name in the kubeconfig to one of its names."
	case errors.As(err, &authorityErr), errors.As(err, &certErr):
		return "The API server's certificate isn't signed by the CA in the kubeconfig."
	case apierrors.IsUnauthorized(err):
		return "The cluster rejected the credentials in the kubeconfig. They may have expired."
	case apierrors.IsNotFound(err):
		return "It doesn't exist (any more). It may have just been deleted."
	case apierrors.IsForbidden(err):
		return "The credentials work, but this user isn't allowed to read this."
	case errors.As(err, &netErr), strings.Contains(err.Error(), "connection refused"), strings.Contains(err.Error(), "no such host"):
		return "Couldn't reach the API server at " + server + ". Check the address and that this machine " +
			"(or container) can reach it."
	default:
		return "Something went wrong talking to the cluster."
	}
}
