package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bardisty/ficha/internal/models"
	"github.com/spf13/cobra"
)

// windowHelp describes the values --since and --until take.
const windowHelp = "a date (2026-09-01), a time (2026-09-01T14:00), today, yesterday, or an age: 7d, 2w, 12h, 30m"

// windowForms says what windowHelp says, wrapped for the help of each
// command that takes --since and --until, whose flag lines are too narrow.
const windowForms = `--since and --until take a date (2026-09-01), a time (2026-09-01T14:00),
today, yesterday, or an age: 7d, 2w, 12h, 30m. A date in --until includes
that whole day.`

// addWindowFlags registers --since and --until, which limit a report to the
// messages timestamped in that range.
func addWindowFlags(cmd *cobra.Command, cfg *config) {
	cmd.Flags().StringVar(&cfg.since, "since", "", "Only count messages from this point on")
	cmd.Flags().StringVar(&cfg.until, "until", "", "Only count messages before this point")
	_ = cmd.RegisterFlagCompletionFunc("since", fixedValues(windowValues))
	_ = cmd.RegisterFlagCompletionFunc("until", fixedValues(windowValues))
}

// timeWindow parses --since and --until against now, in local time.
func (cfg *config) timeWindow(now time.Time) (models.TimeWindow, error) {
	var w models.TimeWindow
	var err error
	if cfg.since != "" {
		if w.Since, err = parseWindowBound(cfg.since, now, false); err != nil {
			return w, usageErrorf("invalid --since %q: %w", cfg.since, err)
		}
	}
	if cfg.until != "" {
		if w.Until, err = parseWindowBound(cfg.until, now, true); err != nil {
			return w, usageErrorf("invalid --until %q: %w", cfg.until, err)
		}
	}
	// A --since still to come is a typo'd year or month far more often than
	// a choice, and its empty report would read as nothing spent. An
	// --until to come is a fair way to say "so far".
	if w.Since.After(now) {
		return w, usageErrorf("--since %s is after now", cfg.since)
	}
	if !w.Since.IsZero() && !w.Until.IsZero() && !w.Since.Before(w.Until) {
		return w, usageErrorf("--since %s is not before --until %s", cfg.since, cfg.until)
	}
	return w, nil
}

// parseWindowBound reads one --since/--until value. A bare date means its
// local midnight, or for --until (end) the midnight after it, so
// "--until 2026-09-28" includes the 28th. An age counts back from now.
func parseWindowBound(s string, now time.Time, end bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	// bound is the start of local date y-m-d, or of the day after it for --until.
	bound := func(y int, m time.Month, d int) time.Time {
		if end {
			d++
		}
		return dayStart(y, m, d)
	}
	switch strings.ToLower(s) {
	case "today":
		return bound(now.Local().Date()), nil
	case "yesterday":
		return bound(now.Local().AddDate(0, 0, -1).Date()), nil
	}
	// Read the date's parts directly: a local time.Date for a midnight that
	// daylight saving skipped would already have moved to the day before.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return bound(t.Date()), nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04Z07:00", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	if age, ok := parseAge(strings.ToLower(s)); ok {
		return now.Add(-age), nil
	}
	return time.Time{}, fmt.Errorf("want %s", windowHelp)
}

// dayStart returns the first instant of local date y-m-d. Where daylight
// saving skips midnight, Go resolves that midnight to the day before, so it
// steps forward until the local date is d. Noon always exists, so it bounds
// the walk.
func dayStart(y int, m time.Month, d int) time.Time {
	noon := time.Date(y, m, d, 12, 0, 0, 0, time.Local)
	y, m, d = noon.Date()
	t := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	for t.Day() != d && t.Before(noon) {
		t = t.Add(time.Minute)
	}
	return t
}

// maxAge bounds an age well past any transcript, and short of where
// time.Duration overflows.
const maxAge = 100 * 365 * 24 * time.Hour

// parseAge reads "7d" and "2w" as well as Go durations ("12h", "30m").
func parseAge(s string) (time.Duration, bool) {
	d, ok := parseAgeUnbounded(s)
	return d, ok && d <= maxAge
}

func parseAgeUnbounded(s string) (time.Duration, bool) {
	// Checked against maxAge in days first: n days as a Duration can
	// overflow before the comparison.
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && strings.HasSuffix(s, "d") && n >= 0 {
		return time.Duration(n) * 24 * time.Hour, n <= int(maxAge/(24*time.Hour))
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "w")); err == nil && strings.HasSuffix(s, "w") && n >= 0 {
		return time.Duration(n) * 7 * 24 * time.Hour, n <= int(maxAge/(7*24*time.Hour))
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return d, true
	}
	return 0, false
}
