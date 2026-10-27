package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// ParseSchedule validates a BACKUP_SCHEDULE value: a standard 5-field cron
// expression ("0 3 * * *"), a descriptor such as "@daily" or "@every 6h", and
// optionally a "CRON_TZ=Europe/Berlin " prefix (which needs time zone data,
// e.g. the time/tzdata package, in the binary). Without CRON_TZ the process's
// local zone is used, which is UTC in the container unless TZ is set.
func ParseSchedule(spec string) (cron.Schedule, error) {
	s, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("BACKUP_SCHEDULE %q: %w", spec, err)
	}
	return s, nil
}

// Schedule validates spec and then, in a background goroutine, calls fn at
// every time the schedule names until ctx is cancelled. Runs never overlap: if
// fn takes longer than the interval, the missed slots are skipped and the next
// run is scheduled from when fn returned. fn receives ctx, so cancelling it
// also asks an in-progress run to stop.
//
// It returns an error only for an invalid spec, so callers can fail fast at
// startup.
func Schedule(ctx context.Context, spec string, fn func(context.Context)) error {
	s, err := ParseSchedule(spec)
	if err != nil {
		return err
	}
	go loop(ctx, s, fn, time.Now)
	return nil
}

func loop(ctx context.Context, s cron.Schedule, fn func(context.Context), clock func() time.Time) {
	for {
		next := s.Next(clock())
		if next.IsZero() {
			return // the schedule never fires again
		}
		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		fn(ctx)
	}
}
