package scheduler

import (
	"testing"
	"time"

	"foldersnap/internal/model"
)

func TestNextDue(t *testing.T) {
	loc := time.FixedZone("test", 3*60*60)
	from := time.Date(2026, time.January, 31, 10, 30, 0, 0, loc)
	cases := []struct {
		name     string
		schedule model.Schedule
		want     time.Time
	}{
		{"daily", model.Schedule{Kind: model.ScheduleDaily, Hour: 9}, time.Date(2026, time.February, 1, 9, 0, 0, 0, loc)},
		{"weekly", model.Schedule{Kind: model.ScheduleWeekly, Weekday: time.Monday, Hour: 8}, time.Date(2026, time.February, 2, 8, 0, 0, 0, loc)},
		{"monthly-clamp", model.Schedule{Kind: model.ScheduleMonthly, DayOfMonth: 31, Hour: 10}, time.Date(2026, time.February, 28, 10, 0, 0, 0, loc)},
		{"interval", model.Schedule{Kind: model.ScheduleInterval, IntervalHours: 3}, from.Add(3 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextDue(tc.schedule, from)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCatchUpProducesOneDecision(t *testing.T) {
	now := time.Now().UTC()
	schedule := model.Schedule{Kind: model.ScheduleInterval, IntervalHours: 1, NextDueAtUTC: now.Add(-24 * time.Hour)}
	if !IsCatchUpDue(schedule, now) {
		t.Fatal("expected catch-up")
	}
	next, err := NextDue(schedule, now)
	if err != nil || !next.After(now) {
		t.Fatalf("invalid next due: %v %v", next, err)
	}
	anchored, err := AdvanceToFuture(schedule, now.Add(-24*time.Hour), now)
	if err != nil || !anchored.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected anchored next due: %v %v", anchored, err)
	}
}
