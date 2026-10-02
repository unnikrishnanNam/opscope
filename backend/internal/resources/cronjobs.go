package resources

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CronJob is one row of the cronjob list.
type CronJob struct {
	Meta
	Schedule     string     `json:"schedule"` // cron syntax, e.g. "0 3 * * *"
	TimeZone     string     `json:"timeZone,omitempty"`
	Suspended    bool       `json:"suspended"`
	Active       int        `json:"active"`       // jobs running right now
	LastSchedule *time.Time `json:"lastSchedule"` // when a job was last started; null if never
	LastSuccess  *time.Time `json:"lastSuccess"`  // when a job last finished successfully
}

func listCronJobs(ctx context.Context, client kubernetes.Interface, namespace string) (any, error) {
	list, err := client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]CronJob, 0, len(list.Items))
	for _, cj := range list.Items {
		row := CronJob{
			Meta:         metaOf(cj.ObjectMeta),
			Schedule:     cj.Spec.Schedule,
			Suspended:    cj.Spec.Suspend != nil && *cj.Spec.Suspend,
			Active:       len(cj.Status.Active),
			LastSchedule: timePtr(cj.Status.LastScheduleTime),
			LastSuccess:  timePtr(cj.Status.LastSuccessfulTime),
		}
		if cj.Spec.TimeZone != nil {
			row.TimeZone = *cj.Spec.TimeZone
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}
