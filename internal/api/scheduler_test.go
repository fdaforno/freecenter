package api

import (
	"testing"
	"time"

	"freecenter/internal/model"
)

func TestNextRun(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("no time zone database")
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, rome)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name string
		sc   model.Schedule
		from string
		want string
	}{
		{"daily later today", model.Schedule{Frequency: "daily", Hour: 23, Minute: 30}, "2026-10-07 10:00", "2026-10-07 23:30"},
		{"daily already passed", model.Schedule{Frequency: "daily", Hour: 2, Minute: 0}, "2026-10-07 10:00", "2026-10-08 02:00"},
		{"daily exactly now moves on", model.Schedule{Frequency: "daily", Hour: 10, Minute: 0}, "2026-10-07 10:00", "2026-10-08 10:00"},
		{"hourly", model.Schedule{Frequency: "hourly", Minute: 15}, "2026-10-07 10:20", "2026-10-07 11:15"},
		{"weekly, Sunday", model.Schedule{Frequency: "weekly", Weekday: 0, Hour: 3, Minute: 0}, "2026-10-07 10:00", "2026-10-11 03:00"},
		{"weekly, same day later", model.Schedule{Frequency: "weekly", Weekday: 3, Hour: 22, Minute: 0}, "2026-10-07 10:00", "2026-10-07 22:00"},
		// 02:30 does not exist on the night clocks go forward... but does on the night they go back (twice): still one run per day.
		{"across the end of DST", model.Schedule{Frequency: "daily", Hour: 12, Minute: 0}, "2026-10-24 13:00", "2026-10-25 12:00"},
	}
	for _, c := range cases {
		c.sc.Timezone = "Europe/Rome"
		if got := nextRun(&c.sc, at(c.from)); !got.Equal(at(c.want)) {
			t.Errorf("%s: got %s, want %s", c.name, got.In(rome).Format("2006-01-02 15:04"), c.want)
		}
	}
	// An unknown zone falls back to UTC instead of failing.
	sc := model.Schedule{Frequency: "daily", Hour: 1, Timezone: "Nowhere/Nope"}
	if got := nextRun(&sc, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)); got.Hour() != 1 || got.Location() != time.UTC {
		t.Errorf("fallback zone: got %s", got)
	}
}
