package executor

import (
	"time"

	"github.com/robfig/cron/v3"
)

// cronWithSeconds accepts 6-field expressions ("*/10 * * * * *"); standard
// 5-field expressions are handled by the fallback in NextCronRun.
var cronWithSeconds = cron.NewParser(
	cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// NextCronRun returns the first fire time after `from` for a 6-field or a
// standard 5-field cron expression.
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
