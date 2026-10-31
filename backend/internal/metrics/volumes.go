package metrics

// Volume usage: how full each PersistentVolumeClaim's volume is.
//
// No Kubernetes object stores this. Each node's kubelet measures the
// volumes mounted there and serves the numbers in its stats summary, which
// Opscope reads through the API server's node proxy:
//
//	GET /api/v1/nodes/{node}/proxy/stats/summary
//
// That needs `get` on `nodes/proxy`, which reaches the whole kubelet API, so
// the server only calls this when OPSCOPE_VOLUME_USAGE=true (see
// docs/deployment.md).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// The kubelet measures volumes about once a minute by default
	// (--volume-stats-agg-period), so asking more often than this would
	// mostly return the same numbers.
	volumeCacheFor = 30 * time.Second
	// A node that doesn't answer in time is reported as unread, rather than
	// holding up the whole page.
	nodeTimeout = 5 * time.Second
)

// ErrVolumeUsageForbidden means volume usage is on, but the credentials
// Opscope uses aren't allowed to read it.
var ErrVolumeUsageForbidden = errors.New("not allowed to read volume usage from the nodes (needs get on nodes/proxy)")

// VolumeUsage is how full one claim's volume is, as the kubelet sees it.
type VolumeUsage struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`       // the claim
	Used       int64  `json:"used"`       // bytes in use
	Capacity   int64  `json:"capacity"`   // the filesystem's size, which can differ a little from the claim's
	Available  int64  `json:"available"`  // bytes still free for pods
	InodesUsed int64  `json:"inodesUsed"` // a volume can be full on inodes with space left
	Inodes     int64  `json:"inodes"`
}

// UnreadNode is a node whose stats couldn't be read, with the claims in use
// there, so the page can say why they have no numbers.
type UnreadNode struct {
	Node   string   `json:"node"`
	Error  string   `json:"error"`
	Claims []string `json:"claims"` // "namespace/name"
}

// VolumeReport is the answer for one namespace, or the whole cluster.
// A claim that's in none of the lists isn't mounted by any running pod, and
// the kubelet only measures volumes that are mounted.
type VolumeReport struct {
	Volumes []VolumeUsage `json:"volumes"`
	Unread  []UnreadNode  `json:"unread"`
	// Claims that are mounted, on nodes that answered, but without numbers:
	// the kubelet doesn't measure every kind of volume (hostPath ones, such
	// as local-path's, aren't), and a volume mounted moments ago has none yet.
	Unmeasured []string `json:"unmeasured"` // "namespace/name"
}

// Volumes reads volume usage and remembers each node's answer for a short
// while, so a big cluster isn't asked for every node on every refresh.
type Volumes struct {
	mu    sync.Mutex
	cache map[string]nodeStats // clusterID + "/" + node name

	// fetch returns a node's raw stats summary; tests replace it.
	fetch func(ctx context.Context, kube kubernetes.Interface, node string) ([]byte, error)
}

// nodeStats is one node's answer: its claims' usage, or why it failed.
type nodeStats struct {
	at      time.Time
	volumes []VolumeUsage
	err     error
}

func NewVolumes() *Volumes {
	return &Volumes{cache: map[string]nodeStats{}, fetch: fetchSummary}
}

// Read returns usage for the claims in a namespace ("" for all). It asks
// only the nodes running pods that mount claims there, all at once.
func (v *Volumes) Read(ctx context.Context, clusterID string, kube kubernetes.Interface, namespace string) (*VolumeReport, error) {
	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	claimsByNode := claimsInUse(pods.Items)
	nodes := make([]string, 0, len(claimsByNode))
	for node := range claimsByNode {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	answers := make([]nodeStats, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i] = v.node(ctx, clusterID, kube, node)
		}()
	}
	wg.Wait()

	report := &VolumeReport{Volumes: []VolumeUsage{}, Unread: []UnreadNode{}, Unmeasured: []string{}}
	seen := map[string]bool{}
	for i, node := range nodes {
		answer := answers[i]
		// RBAC is the same for every node, so one "forbidden" answers for all.
		if apierrors.IsForbidden(answer.err) {
			return nil, fmt.Errorf("%w: %w", ErrVolumeUsageForbidden, answer.err)
		}
		if answer.err != nil {
			report.Unread = append(report.Unread, UnreadNode{Node: node, Error: answer.err.Error(), Claims: claimsByNode[node]})
			continue
		}
		for _, u := range answer.volumes {
			key := u.Namespace + "/" + u.Name
			// A claim mounted on several nodes (ReadWriteMany) is one
			// filesystem, so the first answer is enough.
			if (namespace != "" && u.Namespace != namespace) || seen[key] {
				continue
			}
			seen[key] = true
			report.Volumes = append(report.Volumes, u)
		}
	}
	for i, node := range nodes {
		if answers[i].err != nil {
			continue
		}
		for _, claim := range claimsByNode[node] {
			if !seen[claim] {
				seen[claim] = true
				report.Unmeasured = append(report.Unmeasured, claim)
			}
		}
	}
	slices.Sort(report.Unmeasured)
	sort.Slice(report.Volumes, func(i, j int) bool {
		a, b := report.Volumes[i], report.Volumes[j]
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	return report, nil
}

// node returns one node's usage, from the cache when it's recent enough.
// Failures are remembered too, so a node that's down doesn't make every
// refresh wait for its timeout.
func (v *Volumes) node(ctx context.Context, clusterID string, kube kubernetes.Interface, node string) nodeStats {
	key := clusterID + "/" + node
	v.mu.Lock()
	cached, ok := v.cache[key]
	v.mu.Unlock()
	if ok && Now().Sub(cached.at) < volumeCacheFor {
		return cached
	}

	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()
	answer := nodeStats{at: Now()}
	raw, err := v.fetch(nodeCtx, kube, node)
	if err == nil {
		answer.volumes, err = parseSummary(raw)
	}
	answer.err = err
	if ctx.Err() != nil {
		return answer // the request was cancelled; that says nothing about the node
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.cache[key] = answer
	for k, s := range v.cache {
		if Now().Sub(s.at) >= volumeCacheFor {
			delete(v.cache, k) // includes nodes and clusters that are gone
		}
	}
	return answer
}

// fetchSummary asks a node's kubelet for its stats, through the API server.
func fetchSummary(ctx context.Context, kube kubernetes.Interface, node string) ([]byte, error) {
	body, err := kube.CoreV1().RESTClient().Get().
		AbsPath("/api/v1/nodes", node, "proxy", "stats", "summary").
		DoRaw(ctx)
	if err != nil {
		// The typed client expects protobuf, so the API server's JSON answer
		// to a refused call isn't decoded, and the error just says
		// "unknown". Decode it here, so the message says who isn't allowed
		// what.
		var status metav1.Status
		if json.Unmarshal(body, &status) == nil && status.Kind == "Status" {
			return nil, &apierrors.StatusError{ErrStatus: status}
		}
		return nil, err
	}
	return body, nil
}

// statsSummary is the part of the kubelet's summary we need: each pod's
// volumes, and which claim each one belongs to.
type statsSummary struct {
	Pods []struct {
		Volumes []struct {
			PVCRef *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"pvcRef"`
			UsedBytes      int64 `json:"usedBytes"`
			CapacityBytes  int64 `json:"capacityBytes"`
			AvailableBytes int64 `json:"availableBytes"`
			InodesUsed     int64 `json:"inodesUsed"`
			Inodes         int64 `json:"inodes"`
		} `json:"volume"`
	} `json:"pods"`
}

func parseSummary(raw []byte) ([]VolumeUsage, error) {
	var summary statsSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil, fmt.Errorf("can't read the node's stats summary: %w", err)
	}
	var result []VolumeUsage
	for _, pod := range summary.Pods {
		for _, vol := range pod.Volumes {
			// Volumes without a claim (emptyDir, ConfigMaps, ...) aren't
			// shown, and a volume measured for the first time has no size yet.
			if vol.PVCRef == nil || vol.CapacityBytes == 0 {
				continue
			}
			result = append(result, VolumeUsage{
				Namespace:  vol.PVCRef.Namespace,
				Name:       vol.PVCRef.Name,
				Used:       vol.UsedBytes,
				Capacity:   vol.CapacityBytes,
				Available:  vol.AvailableBytes,
				InodesUsed: vol.InodesUsed,
				Inodes:     vol.Inodes,
			})
		}
	}
	return result, nil
}

// claimsInUse lists, per node, the claims mounted by pods running there:
// "namespace/name", sorted. Generic ephemeral volumes count too; their
// claim is named <pod>-<volume>.
func claimsInUse(pods []corev1.Pod) map[string][]string {
	byNode := map[string][]string{}
	for _, pod := range pods {
		node := pod.Spec.NodeName
		if node == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, vol := range pod.Spec.Volumes {
			var claim string
			switch {
			case vol.PersistentVolumeClaim != nil:
				claim = vol.PersistentVolumeClaim.ClaimName
			case vol.Ephemeral != nil:
				claim = pod.Name + "-" + vol.Name
			default:
				continue
			}
			byNode[node] = append(byNode[node], pod.Namespace+"/"+claim)
		}
	}
	for node, claims := range byNode {
		slices.Sort(claims)
		byNode[node] = slices.Compact(claims)
	}
	return byNode
}
