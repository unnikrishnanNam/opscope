package resources

import (
	"context"
	"sort"
	"time"

	"opscope/internal/kube"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// maxEvents caps how many events we return. Busy clusters can hold thousands;
// the UI only shows the most recent ones.
const maxEvents = 100

// Event is one row of the event list: something that happened to an object,
// like a failed image pull or a pod being scheduled.
type Event struct {
	Meta
	Type    string `json:"type"`   // Normal or Warning
	Reason  string `json:"reason"` // short CamelCase word, e.g. BackOff
	Message string `json:"message"`
	Object  string `json:"object"` // what it happened to, e.g. "Pod/web-7d9f"
	// The same object as parts, so the UI can link to it. ObjectResource is
	// empty for kinds Opscope has no page for (like ReplicaSet).
	ObjectName     string    `json:"objectName"`
	ObjectResource string    `json:"objectResource,omitempty"`
	Count          int32     `json:"count"` // how many times it happened
	LastSeen       time.Time `json:"lastSeen"`
}

// listEvents returns the newest events first. q.Type limits them to one type
// ("Warning" or "Normal").
func listEvents(ctx context.Context, client kubernetes.Interface, q Query) ([]Event, error) {
	options := metav1.ListOptions{}
	if q.Type != "" {
		// A field selector filters on the API server, so only matching events
		// are sent over the network.
		options.FieldSelector = fields.OneTermEqualSelector("type", q.Type).String()
	}

	list, err := client.CoreV1().Events(q.Namespace).List(ctx, options)
	if err != nil {
		return nil, err
	}

	rows := make([]Event, 0, len(list.Items))
	for _, e := range list.Items {
		rows = append(rows, eventRow(&e))
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].LastSeen.After(rows[j].LastSeen) })
	if len(rows) > maxEvents {
		rows = rows[:maxEvents]
	}
	return rows, nil
}

// eventRow turns a Kubernetes event into a row.
func eventRow(e *corev1.Event) Event {
	return Event{
		Meta:           metaOf(e.ObjectMeta),
		Type:           e.Type,
		Reason:         e.Reason,
		Message:        e.Message,
		Object:         e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
		ObjectName:     e.InvolvedObject.Name,
		ObjectResource: resourceForKind(e.InvolvedObject.Kind),
		Count:          kube.EventCount(e),
		LastSeen:       kube.EventLastSeen(e),
	}
}
