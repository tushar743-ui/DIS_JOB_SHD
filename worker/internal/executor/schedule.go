package executor

import (
	"time"

	"github.com/robfig/cron/v3"
)

var cronWithSeconds = cron.NewParser(
	cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

func NextCronRun(expr string, from time.Time) (time.Time, error) {
	if sched, err := cronWithSeconds.Parse(expr); err == nil {
		return sched.Next(from), nil
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}
