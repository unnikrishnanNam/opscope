package collector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeCluster is a pair of client-go fakes that report when each kind's
// watch has started. Informers on fakes miss whatever is created between
// their first list and their watch, so tests wait for the watches before
// changing anything. The probe lists and watches every kind through the
// dynamic fake; the informers use the typed fake, except for Gateway API.
type fakeCluster struct {
	kube *fake.Clientset
	dyn  *dynamicfake.FakeDynamicClient

	mu       sync.Mutex
	watched  map[string]int                        // watches started per resource
	watchers map[string]*watch.RaceFreeFakeWatcher // the latest typed watch per resource
}

func newFakeCluster(objects []runtime.Object, custom ...*unstructured.Unstructured) *fakeCluster {
	listKinds := map[schema.GroupVersionResource]string{}
	isCustom := map[string]bool{}
	for _, k := range resources.Kinds() {
		listKinds[k.GVR] = k.Resource + "List"
		isCustom[k.Resource] = k.Custom
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	for _, obj := range custom {
		gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
		if err := dyn.Tracker().Create(gvr, obj, obj.GetNamespace()); err != nil {
			panic(err)
		}
	}

	f := &fakeCluster{kube: fake.NewClientset(objects...), dyn: dyn, watched: map[string]int{}, watchers: map[string]*watch.RaceFreeFakeWatcher{}}
	f.kube.PrependWatchReactor("*", func(a clienttesting.Action) (bool, watch.Interface, error) {
		inner, err := f.kube.Tracker().Watch(a.GetResource(), a.GetNamespace())
		if err != nil {
			return true, nil, err
		}
		// A watch the test can break (see expireWatch).
		proxy := watch.NewRaceFreeFake()
		go func() {
			for e := range inner.ResultChan() {
				proxy.Action(e.Type, e.Object)
			}
		}()
		f.mu.Lock()
		f.watched[a.GetResource().Resource]++
		f.watchers[a.GetResource().Resource] = proxy
		f.mu.Unlock()
		return true, proxy, nil
	})
	f.dyn.PrependWatchReactor("*", func(a clienttesting.Action) (bool, watch.Interface, error) {
		w, err := f.dyn.Tracker().Watch(a.GetResource(), a.GetNamespace())
		if isCustom[a.GetResource().Resource] {
			f.mu.Lock()
			f.watched[a.GetResource().Resource]++
			f.mu.Unlock()
		}
		return true, w, err
	})
	return f
}

func (f *fakeCluster) clients() Clients {
	return Clients{Kube: f.kube, Dynamic: f.dyn}
}

// waitForWatches waits until every kind's informer is watching. A Gateway
// API kind is watched twice through the dynamic fake: once by the probe,
// then by its informer.
func (f *fakeCluster) waitForWatches(t *testing.T) {
	t.Helper()
	eventually(t, "every kind watched", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, k := range resources.Kinds() {
			want := 1
			if k.Custom {
				want = 2
			}
			if f.watched[k.Resource] < want {
				return false
			}
		}
		return true
	})
}

// expireWatch ends a kind's typed watch the way an API server does when
// the watch has fallen too far behind, which makes the informer list again.
func (f *fakeCluster) expireWatch(resource string) {
	f.mu.Lock()
	w := f.watchers[resource]
	f.mu.Unlock()
	w.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired, Message: "too old resource version"})
}

// fast is the collector's timing shortened for tests.
var fast = timing{
	tick:         10 * time.Millisecond,
	checkEvery:   20 * time.Millisecond,
	reprobeEvery: 100 * time.Millisecond,
	retryFirst:   20 * time.Millisecond,
	pollEvery:    50 * time.Millisecond,
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// recorder reads a subscription and keeps every event it saw.
type recorder struct {
	sub  *bus.Subscription
	all  []bus.ObjectChanged
	gaps []bus.CollectorGap
}

// next returns the next change to resource that isn't from the first list.
func (r *recorder) next(t *testing.T, resource string) bus.ObjectChanged {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-r.sub.C:
			if gap, ok := e.(bus.CollectorGap); ok {
				r.gaps = append(r.gaps, gap)
				continue
			}
			c := e.(bus.ObjectChanged)
			r.all = append(r.all, c)
			if c.Resource == resource && !c.Initial {
				return c
			}
		case <-timeout:
			t.Fatalf("timed out waiting for a change to %s", resource)
		}
	}
}

func pod(name, rv string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: "web", Name: name, UID: types.UID("uid-" + name), ResourceVersion: rv, Labels: labels,
	}}
}

func TestCollectorCachesAndPublishes(t *testing.T) {
	gateway := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata":   map[string]any{"namespace": "web", "name": "main", "uid": "uid-main"},
	}}
	cluster := newFakeCluster([]runtime.Object{
		pod("api-1", "1", nil),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "web", Name: "db", ResourceVersion: "1", Annotations: map[string]string{
				lastAppliedAnnotation: `{"data":{"password":"` + base64.StdEncoding.EncodeToString([]byte("hunter2")) + `"}}`,
			}},
			Data: map[string][]byte{"password": []byte("hunter2")},
		},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "web", Name: "app"}, Data: map[string]string{"mode": "fast"}},
	}, gateway)

	b := bus.New()
	rec := &recorder{sub: b.Subscribe(1000)}
	c := Start(t.Context(), "lab", cluster.clients(), b, quiet)
	cluster.waitForWatches(t)
	eventually(t, "every kind synced", func() bool { return c.Status().State == "live" })

	status := c.Status()
	objects := map[string]int{}
	for _, k := range status.Kinds {
		if k.Mode != "watch" || !k.Synced || k.Error != "" {
			t.Errorf("%s: %+v", k.Resource, k)
		}
		objects[k.Resource] = k.Objects
	}
	if objects["pods"] != 1 || objects["secrets"] != 1 || objects["configmaps"] != 1 || objects["gateways"] != 1 || objects["nodes"] != 0 {
		t.Errorf("objects per kind = %v", objects)
	}

	pods := cluster.kube.CoreV1().Pods("web")
	ctx := context.Background()

	if _, err := pods.Create(ctx, pod("api-2", "1", nil), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	added := rec.next(t, "pods")
	if added.Type != bus.Added || added.Cluster != "lab" || added.UID != "uid-api-2" || added.Name != "api-2" ||
		added.Namespace != "web" || added.New == nil || added.Old != nil || added.Seen.IsZero() {
		t.Errorf("unexpected add: %+v", added)
	}

	// An update that keeps the resourceVersion isn't a change (it's what a
	// relist reports for every object); the next one is.
	if _, err := pods.Update(ctx, pod("api-2", "1", map[string]string{"same": "rv"}), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pods.Update(ctx, pod("api-2", "2", map[string]string{"app": "api"}), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	updated := rec.next(t, "pods")
	if updated.Type != bus.Updated || updated.ResourceVersion != "2" || updated.Old == nil || updated.New == nil {
		t.Errorf("want the update to rv 2 next, got %+v", updated)
	}

	if err := pods.Delete(ctx, "api-2", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	deleted := rec.next(t, "pods")
	if deleted.Type != bus.Deleted || deleted.Name != "api-2" || deleted.Old == nil || deleted.New != nil {
		t.Errorf("unexpected delete: %+v", deleted)
	}

	// A Secret's new value doesn't reach the bus either.
	secret, _ := cluster.kube.CoreV1().Secrets("web").Get(ctx, "db", metav1.GetOptions{})
	secret.Data["password"] = []byte("hunter3")
	secret.ResourceVersion = "2"
	if _, err := cluster.kube.CoreV1().Secrets("web").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	rec.next(t, "secrets")

	// The first list was published as initial adds.
	var initial []string
	for _, e := range rec.all {
		if e.Initial {
			if e.Type != bus.Added {
				t.Errorf("initial event that isn't an add: %+v", e)
			}
			initial = append(initial, e.Resource+"/"+e.Name)
		}
	}
	slices.Sort(initial)
	if want := []string{"configmaps/app", "gateways/main", "pods/api-1", "secrets/db"}; !slices.Equal(initial, want) {
		t.Errorf("initial adds = %v, want %v", initial, want)
	}

	if c.Status().Kinds[slices.IndexFunc(c.Status().Kinds, func(k KindStatus) bool { return k.Resource == "pods" })].LastChange == nil {
		t.Error("pods should have a last change")
	}

	// No Secret value anywhere: not in any event, not in the cache.
	var seen []any
	for _, e := range rec.all {
		seen = append(seen, e.Old, e.New)
	}
	for _, k := range c.kinds {
		seen = append(seen, k.store.List()...)
	}
	everything, err := json.Marshal(seen)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"hunter2", "hunter3"} {
		for _, leak := range []string{value, base64.StdEncoding.EncodeToString([]byte(value))} {
			if strings.Contains(string(everything), leak) {
				t.Errorf("secret value %q reached the bus or the cache", leak)
			}
		}
	}
	// ConfigMaps are cached with their hash, not their values.
	if strings.Contains(string(everything), "fast") || !strings.Contains(string(everything), ContentHashAnnotation) {
		t.Error("the ConfigMap should be cached with a hash instead of its values")
	}
}

func TestStopEndsEveryWatch(t *testing.T) {
	cluster := newFakeCluster(nil)
	c := Start(t.Context(), "lab", cluster.clients(), bus.New(), quiet)
	cluster.waitForWatches(t)

	c.Stop()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the informers didn't stop")
	}
}

func TestTwoClustersOnOneBus(t *testing.T) {
	b := bus.New()
	rec := &recorder{sub: b.Subscribe(1000)}
	for _, name := range []string{"one", "two"} {
		cluster := newFakeCluster(nil)
		Start(t.Context(), name, cluster.clients(), b, quiet)
		cluster.waitForWatches(t)
		if _, err := cluster.kube.CoreV1().Pods("web").Create(context.Background(), pod("in-"+name, "1", nil), metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for range 2 {
		e := rec.next(t, "pods")
		got[e.Cluster] = e.Name
	}
	if got["one"] != "in-one" || got["two"] != "in-two" {
		t.Errorf("pods by cluster = %v", got)
	}
}
