package resources

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Job is one row of the job list.
type Job struct {
	Meta
	Status      string     `json:"status"`      // Complete, Failed, Running, Suspended or Pending
	Completions *int32     `json:"completions"` // how many successes are needed; null = no fixed number
	Succeeded   int32      `json:"succeeded"`
	Failed      int32      `json:"failed"`
	Started     *time.Time `json:"started"`
	Finished    *time.Time `json:"finished"` // null while the job is still going
}

func listJobs(ctx context.Context, client kubernetes.Interface, q Query) ([]Job, error) {
	list, err := client.BatchV1().Jobs(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Job, 0, len(list.Items))
	for _, job := range list.Items {
		status, finished := jobStatus(&job)
		rows = append(rows, Job{
			Meta:        metaOf(job.ObjectMeta),
			Status:      status,
			Completions: job.Spec.Completions,
			Succeeded:   job.Status.Succeeded,
			Failed:      job.Status.Failed,
			Started:     timePtr(job.Status.StartTime),
			Finished:    finished,
		})
	}
	sortRows(rows)
	return rows, nil
}

// jobStatus reads the job's conditions. A finished job has a "Complete" or
// "Failed" condition set to True; the condition's time is when it finished.
func jobStatus(job *batchv1.Job) (string, *time.Time) {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			if job.Status.CompletionTime != nil {
				return "Complete", &job.Status.CompletionTime.Time
			}
			return "Complete", &c.LastTransitionTime.Time
		case batchv1.JobFailed:
			return "Failed", &c.LastTransitionTime.Time
		}
	}

	switch {
	case job.Spec.Suspend != nil && *job.Spec.Suspend:
		return "Suspended", nil
	case job.Status.Active > 0:
		return "Running", nil
	default:
		return "Pending", nil
	}
}
