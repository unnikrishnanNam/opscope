package resources

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
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

func listCronJobs(ctx context.Context, src Source, q Query) ([]CronJob, error) {
	items, err := ListOf[*batchv1.CronJob](ctx, src, "cronjobs", q)
	if err != nil {
		return nil, err
	}

	rows := make([]CronJob, 0, len(items))
	for _, cj := range items {
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
