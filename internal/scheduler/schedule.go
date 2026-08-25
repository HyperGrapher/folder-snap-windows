package scheduler

import (
	"errors"
	"time"

	"foldersnap/internal/model"
)

func NextDue(schedule model.Schedule, from time.Time) (time.Time, error) {
	location := from.Location()
	switch schedule.Kind {
	case model.ScheduleManual:
		return time.Time{}, nil
	case model.ScheduleInterval:
		if schedule.IntervalHours != 1 && schedule.IntervalHours != 3 && schedule.IntervalHours != 6 && schedule.IntervalHours != 12 {
			return time.Time{}, errors.New("unsupported interval")
		}
		return from.Add(time.Duration(schedule.IntervalHours) * time.Hour), nil
	case model.ScheduleDaily:
		candidate := time.Date(from.Year(), from.Month(), from.Day(), schedule.Hour, schedule.Minute, 0, 0, location)
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate, nil
	case model.ScheduleWeekly:
		candidate := time.Date(from.Year(), from.Month(), from.Day(), schedule.Hour, schedule.Minute, 0, 0, location)
		days := (int(schedule.Weekday) - int(candidate.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, days)
		if !candidate.After(from) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate, nil
	case model.ScheduleMonthly:
		if schedule.DayOfMonth < 1 || schedule.DayOfMonth > 31 {
			return time.Time{}, errors.New("invalid day of month")
		}
		candidate := monthlyCandidate(from.Year(), from.Month(), schedule.DayOfMonth, schedule.Hour, schedule.Minute, location)
		if !candidate.After(from) {
			year, month := from.Year(), from.Month()+1
			if month > time.December {
				year, month = year+1, time.January
			}
			candidate = monthlyCandidate(year, month, schedule.DayOfMonth, schedule.Hour, schedule.Minute, location)
		}
		return candidate, nil
	default:
		return time.Time{}, errors.New("unknown schedule kind")
	}
}

func AdvanceAfterDue(schedule model.Schedule, due time.Time) (time.Time, error) {
	return NextDue(schedule, due.Add(time.Nanosecond))
}

func IsCatchUpDue(schedule model.Schedule, now time.Time) bool {
	return schedule.Kind != model.ScheduleManual && !schedule.NextDueAtUTC.IsZero() && !schedule.NextDueAtUTC.After(now.UTC())
}

// AdvanceToFuture preserves the existing interval/calendar anchor while
// collapsing any number of missed runs into one catch-up decision.
func AdvanceToFuture(schedule model.Schedule, due, now time.Time) (time.Time, error) {
	if schedule.Kind == model.ScheduleInterval {
		step := time.Duration(schedule.IntervalHours) * time.Hour
		if step <= 0 {
			return time.Time{}, errors.New("invalid interval")
		}
		if due.After(now) {
			return due, nil
		}
		missed := now.Sub(due)/step + 1
		return due.Add(missed * step), nil
	}
	next := due
	var err error
	for !next.After(now) {
		next, err = NextDue(schedule, next)
		if err != nil {
			return time.Time{}, err
		}
	}
	return next, nil
}

func monthlyCandidate(year int, month time.Month, day, hour, minute int, location *time.Location) time.Time {
	last := time.Date(year, month+1, 0, hour, minute, 0, 0, location).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, hour, minute, 0, 0, location)
}
