package signals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The text the tests' logs carry, which must never be found anywhere the
// engine keeps or says anything.
const secretLine = "db password is hunter2-do-not-keep"

func failingLog() string {
	return strings.Join([]string{
		"starting order-service",
		secretLine,
		"dial tcp 10.96.0.12:5432: connect: connection refused",
		"dial tcp 10.96.0.13:5432: connect: connection refused",
		"giving up after 3 tries",
	}, "\n")
}

func probing(name string) *corev1.Pod {
	p := running(name)
	p.Status.ContainerStatuses[0].Ready = false
	return p
}

func TestLogFindingsBecomeEvidence(t *testing.T) {
	w := testWorld()
	w.upsert("pods", crashing("api-1", 3), t0)
	if got := w.logWants(); !slices.Equal(got, []LogWant{{"uid-api-1", "shop", "api-1", "app", true}}) {
		t.Fatalf("wants = %+v", got)
	}

	// A failing probe's signal doesn't take a finding about a run that ended.
	w.upsert("pods", probing("web-0"), t0)
	if got := w.logWants(); len(got) != 1 {
		t.Fatalf("a running, unready pod without a probe signal wants %+v", got)
	}

	w.setLog("uid-api-1", "app", LogFinding{Previous: true, Read: t0, Lines: 5,
		Patterns: map[string]int{"connection_refused": 2}, Other: 3}, t0.Add(time.Second))
	s := open(w)[0]
	if !strings.HasSuffix(s.Message, "; its log shows connection refused") {
		t.Errorf("message = %q", s.Message)
	}
	var fromLog []Evidence
	for _, e := range s.Evidence {
		if e.Source == FromLog {
			fromLog = append(fromLog, e)
		}
	}
	want := []Evidence{
		{Source: FromLog, Detector: "pods.containers", Field: "log of container app, last run", Pattern: "connection_refused", Matches: 2, Seen: t0},
		{Source: FromLog, Detector: "pods.containers", Field: "log of container app, last run", Value: "3 other kinds of line, not kept", Seen: t0},
	}
	if fmt.Sprint(fromLog) != fmt.Sprint(want) {
		t.Errorf("evidence = %+v", fromLog)
	}

	// A log that couldn't be read says so; the signal stays.
	w.setLog("uid-api-1", "app", LogFinding{Previous: true, Read: t0, Error: "not allowed (needs get on pods/log)"}, t0.Add(2*time.Second))
	s = open(w)[0]
	if last := s.Evidence[len(s.Evidence)-1]; last.Value != "couldn't be read: not allowed (needs get on pods/log)" || s.State != Open {
		t.Errorf("evidence = %+v", s.Evidence)
	}

	// A pod whose probes fail wants its current run's log.
	w.upsert("pods", probing("web-1"), t0)
	w.upsert("events", &corev1.Event{ObjectMeta: objMeta("shop", "web-1.1"), Type: corev1.EventTypeWarning, Reason: "Unhealthy",
		Message:        "Readiness probe failed: HTTP probe failed with statuscode: 503",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "shop", Name: "web-1", UID: "uid-web-1"},
		LastTimestamp:  ago(time.Minute), FirstTimestamp: ago(2 * time.Minute)}, t0)
	if got := w.logWants(); !slices.Contains(got, LogWant{"uid-web-1", "shop", "web-1", "app", false}) {
		t.Errorf("wants = %+v", got)
	}

	// Which run a crash loop reads: the one that just ended. Waiting out
	// its back-off, that's the previous run; shown as terminated, the
	// current one; running again, none.
	between := crashing("api-2", 4)
	between.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
		Reason: "Error", ExitCode: 1, StartedAt: ago(time.Minute), FinishedAt: ago(time.Minute - 2*time.Second)}}
	w.upsert("pods", between, t0)
	if got := w.logWants(); !slices.Contains(got, LogWant{"uid-api-2", "shop", "api-2", "app", false}) {
		t.Errorf("terminated: wants %+v", got)
	}
	again := crashing("api-2", 5)
	again.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	w.upsert("pods", again, t0.Add(time.Second))
	if got := w.logWants(); slices.ContainsFunc(got, func(l LogWant) bool { return l.Pod == "api-2" }) {
		t.Errorf("running again: wants %+v", got)
	}

	// A deleted pod's findings go with it, and a late read is ignored.
	w.remove("pods", "uid-api-1", t0.Add(3*time.Second))
	w.setLog("uid-api-1", "app", LogFinding{Previous: true}, t0.Add(4*time.Second))
	if _, ok := w.logs["uid-api-1"]; ok {
		t.Error("a deleted pod's log findings were kept")
	}
}

// fakeLogs serves the same log for every container, counts reads, and
// notes how many ran at once.
type fakeLogs struct {
	body    string
	err     error
	reads   atomic.Int32
	running atomic.Int32
	most    atomic.Int32
	hold    time.Duration
}

func (f *fakeLogs) read(ctx context.Context, cluster, namespace, pod, container string, previous bool) (io.ReadCloser, error) {
	f.reads.Add(1)
	n := f.running.Add(1)
	defer f.running.Add(-1)
	for {
		m := f.most.Load()
		if n <= m || f.most.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(f.hold)
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.body)), nil
}

// logEngine is an engine on a fake cluster with crash-looping pods, its
// log reader set, and its process log captured at debug level.
func logEngine(t *testing.T, pods int, reader LogReader) (*Engine, *liveWorld, *bytes.Buffer) {
	t.Helper()
	var objects []runtime.Object
	for i := range pods {
		objects = append(objects, crashing(fmt.Sprintf("api-%d", i), 3))
	}
	f := newFakeFeed()
	f.cluster("lab", map[string][]runtime.Object{"pods": objects}, nil)
	var out bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{&out, &mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := NewEngine(f, Default, logger)
	e.SetLogReader(reader)
	l := e.live("lab")
	if l == nil {
		t.Fatal("no live world")
	}
	return e, l, &out
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// No log line is kept: not in the engine's memory, not in any answer, not
// in the bus's events, not in Opscope's own log.
func TestNoLogTextIsKept(t *testing.T) {
	fake := &fakeLogs{body: failingLog()}
	e, l, processLog := logEngine(t, 3, fake.read)
	heard := e.feed.(*fakeFeed).bus.Subscribe(256)
	defer heard.Close()

	e.readClusterLogs(context.Background(), "lab", l)
	if fake.reads.Load() != 3 {
		t.Fatalf("read %d logs", fake.reads.Load())
	}

	var signals []Signal
	l.mu.Lock()
	signals = View{l.w, e.now()}.Signals(Filter{})
	kept := fmt.Sprintf("%#v %#v %#v %#v", l.w.logs, l.w.signals, l.w.history, l.w.health)
	l.mu.Unlock()
	answer, _ := json.Marshal(signals)
	var events strings.Builder
	for len(heard.C) > 0 {
		fmt.Fprintf(&events, "%#v\n", <-heard.C)
	}

	if !strings.Contains(string(answer), `"pattern":"connection_refused","matches":2`) {
		t.Fatalf("the finding didn't reach the signals: %s", answer)
	}
	for name, text := range map[string]string{"memory": kept, "answer": string(answer), "bus": events.String(), "process log": processLog.String()} {
		for _, line := range strings.Split(failingLog(), "\n") {
			if strings.Contains(text, line) || strings.Contains(text, "hunter2") {
				t.Errorf("the %s holds log text %q", name, line)
			}
		}
	}
}

func TestLogReadsAreLimited(t *testing.T) {
	fake := &fakeLogs{body: failingLog(), hold: 20 * time.Millisecond}
	e, l, _ := logEngine(t, 12, fake.read)
	e.readClusterLogs(context.Background(), "lab", l)
	if n := fake.reads.Load(); n != 12 {
		t.Errorf("read %d of 12", n)
	}
	if most := fake.most.Load(); most > logParallel {
		t.Errorf("%d reads at once", most)
	}

	// Again at once, and four minutes on: nothing is read twice within
	// five minutes.
	e.readClusterLogs(context.Background(), "lab", l)
	e.now = func() time.Time { return time.Now().Add(4 * time.Minute) }
	e.readClusterLogs(context.Background(), "lab", l)
	if n := fake.reads.Load(); n != 12 {
		t.Errorf("read %d times", n)
	}
	e.now = func() time.Time { return time.Now().Add(logEvery + time.Second) }
	e.readClusterLogs(context.Background(), "lab", l)
	if n := fake.reads.Load(); n != 24 {
		t.Errorf("after five minutes: %d reads", n)
	}
}

// Without pods/log, the signals stay and say their logs couldn't be read;
// an error's own message, which may quote anything, isn't repeated.
func TestALogThatCantBeRead(t *testing.T) {
	fake := &fakeLogs{err: apierrors.NewForbidden(schema.GroupResource{Resource: "pods/log"}, "api-0", fmt.Errorf("%s", secretLine))}
	e, l, processLog := logEngine(t, 1, fake.read)
	e.readClusterLogs(context.Background(), "lab", l)
	l.mu.Lock()
	s := View{l.w, e.now()}.Signals(Filter{State: Open})
	l.mu.Unlock()
	if len(s) != 1 || s[0].Type != "crash_loop" {
		t.Fatalf("signals = %+v", s)
	}
	last := s[0].Evidence[len(s[0].Evidence)-1]
	if last.Value != "couldn't be read: not allowed (needs get on pods/log)" {
		t.Errorf("evidence = %+v", last)
	}
	if strings.Contains(processLog.String(), "hunter2") {
		t.Error("the error's message reached the process log")
	}
}

// With no reader set (OPSCOPE_LOG_SIGNALS=false), no log is read.
func TestLogSignalsOff(t *testing.T) {
	f := newFakeFeed()
	f.cluster("lab", map[string][]runtime.Object{"pods": {crashing("api-1", 1)}}, nil)
	e := NewEngine(f, Default, quiet)
	if e.readLog != nil {
		t.Fatal("a reader is set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}
