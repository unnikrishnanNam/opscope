package diagnose

import (
	"fmt"

	"opscope/internal/graph"

	corev1 "k8s.io/api/core/v1"
)

// ---- pvc.pending: a claim isn't bound to a volume ----

var pvcPending = Rule{ID: "pvc.pending", Resources: []string{"persistentvolumeclaims"}, Check: func(c *Context, n *graph.Node) []*Finding {
	pvc, _ := c.Object(n).(*corev1.PersistentVolumeClaim)
	if pvc == nil {
		return nil
	}
	if pvc.Status.Phase == corev1.ClaimLost {
		f := &Finding{Severity: Failing, Confidence: Certain,
			Title:  fmt.Sprintf("Its volume %s is gone", pvc.Spec.VolumeName),
			Detail: "The PersistentVolume it was bound to no longer exists. Its data may be lost."}
		if volume := c.Node(key("persistentvolumes", "", pvc.Spec.VolumeName)); volume != nil {
			f.Blames = []string{volume.ID}
		}
		return []*Finding{f}
	}
	if pvc.Status.Phase != corev1.ClaimPending || n.State == graph.OK {
		return nil // bound, or waiting for a pod to use it, as its class wants
	}

	events := c.Events(pvc.UID, "ProvisioningFailed")
	f := &Finding{
		Severity:   Failing,
		Confidence: Certain,
		Evidence:   eventEvidence(events),
		Next:       []Step{eventsOf(n), {Label: "kubectl get storageclasses", Command: "kubectl get storageclasses"}},
	}
	var class *graph.Node
	for _, e := range c.G.Out(n.ID) {
		if target := c.Node(e.To); target.Resource == "storageclasses" {
			class = target
		}
	}
	switch {
	case class != nil && class.State == graph.Missing:
		f.Title = fmt.Sprintf("It asks for StorageClass %s, which doesn't exist", class.Name)
		f.Detail = "Nothing provisions a volume for it, so it stays Pending, and so do the pods that need it."
		f.Blames = []string{class.ID}
	case class == nil && pvc.Spec.VolumeName == "":
		f.Title = "It names no StorageClass, and the cluster has no default"
		f.Detail = "Without a class, it waits for someone to create a PersistentVolume that matches it by hand."
	case len(events) > 0:
		f.Title = "Its volume can't be provisioned"
		f.Detail = events[0].Message
		f.Confidence = Likely
		if class != nil {
			f.Blames = []string{class.ID}
		}
	case n.State == graph.Failing:
		f.Title = "It has waited for a volume for more than 5 minutes"
		f.Detail = "Its StorageClass's provisioner hasn't created one. Its events may say why."
		f.Confidence = Possible
	default:
		return nil // still within its first minutes
	}
	return []*Finding{f}
}}
