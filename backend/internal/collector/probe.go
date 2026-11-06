package collector

import (
	"context"
	"errors"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opscope/internal/resources"
)

// probe finds how a kind can be read: a list of one object, then a short
// watch. It changes nothing in the cluster. An error means the cluster gave
// no usable answer (unreachable, timed out), and the kind stays pending.
func (c *Collector) probe(ctx context.Context, k *kind) (mode, reason string, err error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	client := c.clients.Dynamic.Resource(k.GVR)
	list, err := client.List(ctx, metav1.ListOptions{Limit: 1})
	switch {
	case apierrors.IsNotFound(err):
		return ModeNotInstalled, "not installed on this cluster", nil
	case apierrors.IsForbidden(err) && k.Namespaced:
		// Some namespaces may still be readable one at a time, which is
		// what pages do in direct mode.
		return ModeDirect, "listing across all namespaces isn't allowed", nil
	case apierrors.IsForbidden(err):
		return ModeForbidden, "listing isn't allowed", nil
	case err != nil:
		return ModePending, "", err
	}

	timeout := int64(1)
	w, err := client.Watch(ctx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion(), TimeoutSeconds: &timeout})
	if apierrors.IsForbidden(err) {
		return ModePoll, "watching isn't allowed", nil
	}
	if err == nil {
		w.Stop()
	}
	// Any other watch error is the informer's to retry.
	return ModeWatch, "", nil
}

// isPermissionOrMissing reports errors that a new probe may answer
// differently from the cluster's side: a permission taken away, or a kind
// removed (Gateway API uninstalled).
func isPermissionOrMissing(err error) bool {
	return apierrors.IsForbidden(err) || apierrors.IsNotFound(err) || errors.Is(err, resources.ErrGatewayAPINotInstalled)
}

// probeAll probes every kind that's due, all at once, and switches the
// ones whose mode changed. force probes every kind, as at the start.
func (c *Collector) probeAll(ctx context.Context, force bool) {
	now := time.Now()
	type answer struct {
		mode, reason string
		err          error
	}
	answers := make([]*answer, len(c.kinds))
	done := make(chan struct{}, len(c.kinds))
	n := 0
	for i, k := range c.kinds {
		k.mu.Lock()
		due := force || (!k.nextProbe.IsZero() && !now.Before(k.nextProbe))
		k.mu.Unlock()
		if !due {
			continue
		}
		n++
		go func() {
			mode, reason, err := c.probe(ctx, k)
			answers[i] = &answer{mode, reason, err}
			done <- struct{}{}
		}()
	}
	for range n {
		select {
		case <-done:
		case <-ctx.Done():
			return
		}
	}

	changed := map[string][]string{} // resources by their new mode
	for i, k := range c.kinds {
		a := answers[i]
		if a == nil {
			continue
		}
		k.mu.Lock()
		current := k.mode
		if a.err != nil {
			// No answer: try again soon, then less and less often.
			k.failures++
			k.lastErr = a.err
			k.nextProbe = now.Add(min(c.timing.retryFirst<<min(k.failures-1, 10), c.timing.reprobeEvery))
			k.mu.Unlock()
			continue
		}
		k.failures = 0
		if a.mode == ModeWatch {
			k.nextProbe = time.Time{} // watched: probed again only if something fails
		} else {
			k.nextProbe = now.Add(c.timing.reprobeEvery)
		}
		if a.mode == current {
			k.reason = a.reason
			k.mu.Unlock()
			continue
		}
		k.mu.Unlock()
		k.setMode(ctx, a.mode, a.reason)
		changed[a.mode] = append(changed[a.mode], k.Resource)
	}
	c.logModes(changed, force)
}

// logModes says in one line which kinds changed how they're read. Kinds
// that aren't watched are worth an info line; everything being watched, or
// a kind the cluster doesn't have, only at debug level.
func (c *Collector) logModes(changed map[string][]string, first bool) {
	if len(changed) == 0 {
		return
	}
	var attrs []any
	notable := false
	for _, mode := range []string{ModeWatch, ModePoll, ModeDirect, ModeForbidden, ModeNotInstalled} {
		if resources := changed[mode]; len(resources) > 0 {
			attrs = append(attrs, mode, strings.Join(resources, ","))
			notable = notable || mode == ModePoll || mode == ModeDirect || mode == ModeForbidden ||
				(mode == ModeWatch && !first) // watched again after a permission was granted
		}
	}
	if notable {
		c.logger.Info("how kinds are read", attrs...)
	} else {
		c.logger.Debug("how kinds are read", attrs...)
	}
}
