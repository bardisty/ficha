package cmd

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

// pinLocal sets time.Local, which TZ sets for ficha, to UTC for one test.
func pinLocal(t *testing.T) {
	t.Helper()
	saved := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = saved })
}

// --by-day adds the days to summary's and global's table and json, and the
// rows add up to the report's total; csv stays as it is, and stderr says
// the flag did nothing there.
func TestE2EByDay(t *testing.T) {
	setupE2EFixture(t)
	pinLocal(t)
	type day struct {
		Date         string `json:"date"`
		SessionCount int    `json:"session_count"`
		MessageCount int    `json:"message_count"`
		TotalCost    struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"total_cost"`
	}
	type report struct {
		MessageCount int `json:"message_count"`
		TotalCost    struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"total_cost"`
		Days []day `json:"days"`
	}
	check := func(t *testing.T, r report, wantDates ...string) {
		t.Helper()
		var dates []string
		n, sum := 0, 0.0
		for _, d := range r.Days {
			dates = append(dates, d.Date)
			n += d.MessageCount
			sum += d.TotalCost.TotalCost
		}
		if !slices.Equal(dates, wantDates) {
			t.Errorf("days %v, want %v", dates, wantDates)
		}
		if n != r.MessageCount || math.Abs(sum-r.TotalCost.TotalCost) > 1e-9 {
			t.Errorf("days hold %d messages costing %v; the report has %d and %v", n, sum, r.MessageCount, r.TotalCost.TotalCost)
		}
	}
	run := func(t *testing.T, args ...string) (string, string) {
		t.Helper()
		out, stderr, err := executeCLISplit(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr)
		}
		return out, stderr
	}

	t.Run("summary json", func(t *testing.T) {
		out, _ := run(t, "summary", projFlag, "--by-day", "-f", "json")
		var r report
		mustJSON(t, out, &r)
		check(t, r, "2026-02-02", "2026-02-01")
		var detail struct {
			Summary report `json:"summary"`
		}
		out, _ = run(t, "summary", projFlag, "-d", "--by-day", "-f", "json")
		mustJSON(t, out, &detail)
		check(t, detail.Summary, "2026-02-02", "2026-02-01")
		out, _ = run(t, "summary", projFlag, "-f", "json")
		if strings.Contains(out, `"days"`) {
			t.Errorf("days without --by-day:\n%s", out)
		}
	})

	t.Run("global json", func(t *testing.T) {
		out, _ := run(t, "global", "--by-day", "-f", "json")
		var r report
		mustJSON(t, out, &r)
		check(t, r, "2026-02-03", "2026-02-02", "2026-02-01")
		out, _ = run(t, "global", "--by-day", "--since", "2026-02-02", "-f", "json")
		r = report{}
		mustJSON(t, out, &r)
		check(t, r, "2026-02-03", "2026-02-02")
	})

	t.Run("table", func(t *testing.T) {
		for _, args := range [][]string{
			{"summary", projFlag, "--by-day"},
			{"summary", projFlag, "-d", "--by-day"},
			{"global", "--by-day"},
		} {
			out, _ := run(t, append(args, "--no-color")...)
			plain, _ := run(t, append(args[:len(args)-1:len(args)-1], "--no-color")...)
			if !strings.Contains(out, "COST BY DAY") || !strings.Contains(out, "Mon Feb 02") || !strings.Contains(out, "Sun Feb 01") {
				t.Errorf("%v: no day rows:\n%s", args, out)
			}
			if strings.Contains(plain, "COST BY DAY") {
				t.Errorf("%v: days without --by-day", args)
			}
			// The section is the only addition.
			start := strings.LastIndex(out[:strings.Index(out, "COST BY DAY")], "\n\n")
			rows := strings.Index(out, "\n  DAY ")
			end := rows + strings.Index(out[rows:], "\n\n")
			if got := out[:start] + out[end:]; got != plain {
				t.Errorf("%v: --by-day changed more than its section:\n%s\nwithout it:\n%s", args, got, plain)
			}
		}
	})

	t.Run("csv", func(t *testing.T) {
		for _, args := range [][]string{{"summary", projFlag}, {"summary", projFlag, "-d"}, {"global"}} {
			plain, _ := run(t, append(slices.Clone(args), "-f", "csv")...)
			out, stderr := run(t, append(slices.Clone(args), "-f", "csv", "--by-day")...)
			if out != plain {
				t.Errorf("%v: --by-day changed csv", args)
			}
			if !strings.Contains(stderr, "--by-day has no effect on csv output") {
				t.Errorf("%v: no warning on stderr: %q", args, stderr)
			}
		}
	})

	t.Run("completion", func(t *testing.T) {
		for _, c := range []string{"summary", "global"} {
			out, err := executeLikeMain(t, "__complete", c, "--")
			if err != nil || !strings.Contains(out, "--by-day\t") {
				t.Errorf("%s doesn't complete --by-day: %v\n%s", c, err, out)
			}
		}
	})
}
