package clusters

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// requestTimeout stops a call to an unreachable cluster from hanging forever.
const requestTimeout = 10 * time.Second

// ContextInfo describes one context found in a kubeconfig.
type ContextInfo struct {
	Name   string `json:"name"`
	Server string `json:"server"`
}

// parseKubeconfig turns kubeconfig YAML into client-go's config struct.
func parseKubeconfig(data []byte) (*clientcmdapi.Config, error) {
	config, err := clientcmd.Load(data)
	if err != nil {
		return nil, fmt.Errorf("this doesn't look like a valid kubeconfig: %w", err)
	}
	if len(config.Contexts) == 0 {
		return nil, errors.New("the kubeconfig has no contexts")
	}
	return config, nil
}

// ListContexts returns every context in a kubeconfig, sorted by name, plus
// the name of the kubeconfig's current context (may be empty).
func ListContexts(data []byte) ([]ContextInfo, string, error) {
	config, err := parseKubeconfig(data)
	if err != nil {
		return nil, "", err
	}

	contexts := []ContextInfo{}
	for name, ctx := range config.Contexts {
		info := ContextInfo{Name: name}
		if cluster, ok := config.Clusters[ctx.Cluster]; ok {
			info.Server = cluster.Server
		}
		contexts = append(contexts, info)
	}
	sort.Slice(contexts, func(i, j int) bool { return contexts[i].Name < contexts[j].Name })

	return contexts, config.CurrentContext, nil
}

// minify keeps only the chosen context (and its cluster and user) and makes it
// the current context. This way we never store credentials for clusters the
// user didn't pick. An empty contextName means "the current context".
func minify(config *clientcmdapi.Config, contextName string) (*clientcmdapi.Config, error) {
	config = config.DeepCopy()
	if contextName != "" {
		config.CurrentContext = contextName
	}
	if config.CurrentContext == "" {
		return nil, errors.New("no context chosen and the kubeconfig has no current-context")
	}
	if _, ok := config.Contexts[config.CurrentContext]; !ok {
		return nil, fmt.Errorf("context %q not found in the kubeconfig", config.CurrentContext)
	}
	if err := clientcmdapi.MinifyConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

// checkSafeForUpload rejects kubeconfig features that are fine on your own
// machine but dangerous when the file arrives through the web UI:
//
//   - "exec" and "auth-provider" run a program on the server to get a token,
//     so a pasted file could run any command inside OpScope.
//   - file paths (client-certificate, token-file, ...) would make OpScope read
//     files from its own disk.
//
// Kubeconfigs with everything embedded (like kubeadm's admin.conf) pass.
// Tip for users: `kubectl config view --minify --flatten` embeds everything.
func checkSafeForUpload(config *clientcmdapi.Config) error {
	ctx := config.Contexts[config.CurrentContext]

	if user, ok := config.AuthInfos[ctx.AuthInfo]; ok {
		if user.Exec != nil || user.AuthProvider != nil {
			return errors.New("kubeconfigs that run a command or auth plugin to log in can't be added from the UI; " +
				"use OPSCOPE_KUBECONFIG for those")
		}
		if user.ClientCertificate != "" || user.ClientKey != "" || user.TokenFile != "" {
			return errors.New("the user entry points to files on disk; embed them with `kubectl config view --minify --flatten`")
		}
	}
	if cluster, ok := config.Clusters[ctx.Cluster]; ok && cluster.CertificateAuthority != "" {
		return errors.New("the cluster entry points to a CA file on disk; embed it with `kubectl config view --minify --flatten`")
	}
	return nil
}

// clientSet holds the clients OpScope keeps for one cluster.
type clientSet struct {
	kube   *kubernetes.Clientset  // typed: Go structs for built-in kinds (Pods, Services, ...)
	dyn    *dynamic.DynamicClient // dynamic: any kind as plain maps, incl. custom resources
	stream *kubernetes.Clientset  // typed, but with no timeout: for long streams like following logs
}

// newClients builds the clients for the current context of config. It does
// not contact the cluster yet. It also returns the API server address.
func newClients(config *clientcmdapi.Config) (clientSet, string, error) {
	restConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return clientSet{}, "", fmt.Errorf("can't build a client from this kubeconfig: %w", err)
	}
	return clientsFor(restConfig)
}

// clientsFor builds the clients from connection settings, wherever they came
// from: a kubeconfig, or the pod's own service account (in-cluster mode).
func clientsFor(restConfig *rest.Config) (clientSet, string, error) {
	// The stream client is a copy made before the timeout is set: a timeout
	// covers the whole response, which would cut a log stream after 10 seconds.
	// Streams end instead when the browser goes away (the request context).
	streamConfig := rest.CopyConfig(restConfig)
	restConfig.Timeout = requestTimeout

	var c clientSet
	var err error
	if c.kube, err = kubernetes.NewForConfig(restConfig); err != nil {
		return clientSet{}, "", err
	}
	if c.dyn, err = dynamic.NewForConfig(restConfig); err != nil {
		return clientSet{}, "", err
	}
	if c.stream, err = kubernetes.NewForConfig(streamConfig); err != nil {
		return clientSet{}, "", err
	}
	return c, restConfig.Host, nil
}
