package bus

import (
	"sync"
	"testing"
)

func changed(name string) ObjectChanged {
	return ObjectChanged{Cluster: "lab", Resource: "pods", Type: Added, Name: name}
}

func TestEverySubscriberGetsEveryEvent(t *testing.T) {
	b := New()
	one, two := b.Subscribe(4), b.Subscribe(4)
	b.Publish(changed("a"))
	b.Publish(changed("b"))

	for _, s := range []*Subscription{one, two} {
		for _, want := range []string{"a", "b"} {
			got := (<-s.C).(ObjectChanged)
			if got.Name != want {
				t.Errorf("got %q, want %q", got.Name, want)
			}
		}
	}
}

func TestCloseStopsDeliveryAndClosesTheChannel(t *testing.T) {
	b := New()
	s := b.Subscribe(4)
	s.Close()
	s.Close() // a second Close is harmless
	b.Publish(changed("a"))
	if _, open := <-s.C; open {
		t.Error("the channel should be closed, and empty")
	}
}

// A subscriber that falls behind loses events, and is told so with a gap
// before the next event that fits.
func TestLaggingSubscriberGetsAGap(t *testing.T) {
	b := New()
	slow := b.Subscribe(2)
	fast := b.Subscribe(10)

	b.Publish(changed("a"))
	b.Publish(changed("b")) // slow's queue is full now
	b.Publish(changed("c")) // lost for slow

	names := func(s *Subscription, n int) []string {
		var out []string
		for range n {
			switch e := (<-s.C).(type) {
			case ObjectChanged:
				out = append(out, e.Name)
			case CollectorGap:
				if e.Reason != GapLagged || e.From.IsZero() || e.To.Before(e.From) {
					t.Errorf("unexpected gap %+v", e)
				}
				out = append(out, "gap")
			}
		}
		return out
	}
	if got := names(slow, 2); got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
	b.Publish(changed("d")) // the gap goes first, then d
	if got := names(slow, 2); got[0] != "gap" || got[1] != "d" {
		t.Errorf("got %v, want [gap d]", got)
	}

	// The fast subscriber wasn't affected.
	if len(fast.C) != 4 {
		t.Errorf("fast subscriber has %d events, want 4", len(fast.C))
	}
	fast.Close()
	slow.Close()

	// With room for the gap but not the event, both wait: the event is lost
	// and the gap grows to cover it.
	s := b.Subscribe(2)
	b.Publish(changed("e"))
	b.Publish(changed("f"))
	b.Publish(changed("g")) // lost
	names(s, 1)             // e; one slot free
	b.Publish(changed("h")) // lost: no room for the gap and h together
	names(s, 1)             // f; two slots free
	b.Publish(changed("i"))
	if got := names(s, 2); got[0] != "gap" || got[1] != "i" {
		t.Errorf("got %v, want [gap i]", got)
	}

	// A queue of one is made two, so a reader that keeps pace recovers.
	tiny := b.Subscribe(1)
	b.Publish(changed("j"))
	b.Publish(changed("k"))
	b.Publish(changed("l")) // lost
	names(tiny, 2)          // j, k
	b.Publish(changed("m"))
	if got := names(tiny, 2); got[0] != "gap" || got[1] != "m" {
		t.Errorf("got %v, want [gap m]", got)
	}
}

// Publishing, subscribing and closing from many goroutines at once must
// not race or panic on a closed channel (run with -race).
func TestConcurrentUse(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				b.Publish(changed("x"))
			}
		})
		wg.Go(func() {
			for range 20 {
				s := b.Subscribe(2)
				<-s.C
				s.Close()
			}
		})
	}
	// Keep publishing until every subscriber got its first event.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-done:
			return
		default:
			b.Publish(changed("y"))
		}
	}
}
