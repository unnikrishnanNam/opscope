// Package clusters keeps track of the Kubernetes clusters OpScope knows
// about and holds one client per cluster.
//
// Clusters come from two places:
//   - the environment (OPSCOPE_KUBECONFIG), loaded once at startup
//   - the web UI, saved as files in the data folder so they survive restarts
package clusters

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Where a cluster came from.
const (
	SourceEnv = "env"
	SourceUI  = "ui"
)

// Cluster is one cluster OpScope can talk to. The exported fields are safe
// to send to the browser; credentials stay inside the client.
type Cluster struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Context string `json:"context"`
	Server  string `json:"server"`

	Client  *kubernetes.Clientset `json:"-"` // typed client for built-in kinds
	Dynamic dynamic.Interface     `json:"-"` // dynamic client for custom resources (Gateway API)
	Stream  *kubernetes.Clientset `json:"-"` // typed client without a timeout, for log streams
}

// savedCluster is the file format for clusters added in the UI.
type savedCluster struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Context    string `json:"context"`
	Kubeconfig string `json:"kubeconfig"` // minified: only this cluster's context, cluster and user
}

// Manager holds all known clusters. It is safe to use from many HTTP
// requests at once thanks to the mutex.
type Manager struct {
	mu       sync.RWMutex
	clusters map[string]*Cluster
	dir      string // folder where UI-added clusters are saved
}

// NewManager creates an empty manager that saves clusters in dataDir/clusters.
func NewManager(dataDir string) *Manager {
	return &Manager{
		clusters: map[string]*Cluster{},
		dir:      filepath.Join(dataDir, "clusters"),
	}
}

// LoadFromFile adds the cluster from a kubeconfig file given in the
// environment. contextName may be empty to use the file's current context,
// and name may be empty to use the context name as the display name.
func (m *Manager) LoadFromFile(path, contextName, name string) (*Cluster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config, err := parseKubeconfig(data)
	if err != nil {
		return nil, err
	}
	config, err = minify(config, contextName)
	if err != nil {
		return nil, err
	}

	clients, server, err := newClients(config)
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = config.CurrentContext
	}
	cluster := &Cluster{
		ID:      slugify(name),
		Name:    name,
		Source:  SourceEnv,
		Context: config.CurrentContext,
		Server:  server,
		Client:  clients.kube,
		Dynamic: clients.dyn,
		Stream:  clients.stream,
	}
	m.mu.Lock()
	m.clusters[cluster.ID] = cluster
	m.mu.Unlock()
	return cluster, nil
}

// LoadSaved reads every cluster saved from the UI. A broken file is reported
// in the returned errors but doesn't stop the others from loading.
func (m *Manager) LoadSaved() []error {
	files, err := filepath.Glob(filepath.Join(m.dir, "*.json"))
	if err != nil {
		return []error{err}
	}

	var problems []error
	for _, file := range files {
		if err := m.loadSavedFile(file); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", filepath.Base(file), err))
		}
	}
	return problems
}

func (m *Manager) loadSavedFile(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var saved savedCluster
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	config, err := parseKubeconfig([]byte(saved.Kubeconfig))
	if err != nil {
		return err
	}
	clients, server, err := newClients(config)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, taken := m.clusters[saved.ID]; taken {
		return fmt.Errorf("id %q is already used by the environment cluster, skipping", saved.ID)
	}
	m.clusters[saved.ID] = &Cluster{
		ID:      saved.ID,
		Name:    saved.Name,
		Source:  SourceUI,
		Context: saved.Context,
		Server:  server,
		Client:  clients.kube,
		Dynamic: clients.dyn,
		Stream:  clients.stream,
	}
	return nil
}

// Add checks a kubeconfig from the UI, makes sure the cluster answers, then
// saves it to disk and starts using it.
func (m *Manager) Add(name string, kubeconfig []byte, contextName string) (*Cluster, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("please give the cluster a name")
	}
	id := slugify(name)
	if id == "" {
		return nil, errors.New("the name needs at least one letter or number")
	}
	if _, exists := m.Get(id); exists {
		return nil, fmt.Errorf("a cluster called %q already exists", name)
	}

	config, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	config, err = minify(config, contextName)
	if err != nil {
		return nil, err
	}
	if err := checkSafeForUpload(config); err != nil {
		return nil, err
	}

	clients, server, err := newClients(config)
	if err != nil {
		return nil, err
	}
	cluster := &Cluster{
		ID:      id,
		Name:    name,
		Source:  SourceUI,
		Context: config.CurrentContext,
		Server:  server,
		Client:  clients.kube,
		Dynamic: clients.dyn,
		Stream:  clients.stream,
	}

	// Only save clusters we can actually talk to.
	if _, err := cluster.Version(); err != nil {
		return nil, &ConnectionError{Server: server, Err: err}
	}

	minified, err := clientcmd.Write(*config)
	if err != nil {
		return nil, err
	}
	if err := m.save(savedCluster{ID: id, Name: name, Context: cluster.Context, Kubeconfig: string(minified)}); err != nil {
		return nil, fmt.Errorf("couldn't save the cluster: %w", err)
	}

	m.mu.Lock()
	m.clusters[id] = cluster
	m.mu.Unlock()
	return cluster, nil
}

// save writes one cluster file that only the OpScope user can read (0600),
// inside a folder only it can open (0700).
func (m *Manager) save(saved savedCluster) error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.filePath(saved.ID), data, 0o600)
}

// Remove deletes a cluster that was added in the UI.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cluster, ok := m.clusters[id]
	if !ok {
		return ErrNotFound
	}
	if cluster.Source != SourceUI {
		return errors.New("this cluster comes from the environment; change OPSCOPE_KUBECONFIG to remove it")
	}
	if err := os.Remove(m.filePath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delete(m.clusters, id)
	return nil
}

// ErrNotFound is returned when no cluster has the given id.
var ErrNotFound = errors.New("cluster not found")

// Get returns the cluster with the given id.
func (m *Manager) Get(id string) (*Cluster, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cluster, ok := m.clusters[id]
	return cluster, ok
}

// List returns all clusters: environment ones first, then by name.
func (m *Manager) List() []*Cluster {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*Cluster, 0, len(m.clusters))
	for _, cluster := range m.clusters {
		list = append(list, cluster)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Source != list[j].Source {
			return list[i].Source == SourceEnv
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// Version asks the cluster for its Kubernetes version. It is also our
// "is this cluster reachable?" check.
func (c *Cluster) Version() (string, error) {
	info, err := c.Client.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}
	return info.GitVersion, nil
}

func (m *Manager) filePath(id string) string {
	return filepath.Join(m.dir, id+".json")
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a display name into a URL-friendly id: "My Cluster!" -> "my-cluster".
func slugify(name string) string {
	return strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
}
