package analyzer

import (
	"fmt"
	"sort"
	"time"

	"github.com/bardisty/ficha/internal/models"
)

// dayTotals adds up spend by local calendar day for a --by-day report. It
// sums in the order it's handed sessions and days, which callers keep fixed,
// so the totals don't change from run to run.
type dayTotals map[string]*models.DayCost

// dayKey is the local calendar day t falls on, as DayCost.Date spells it:
// its date in time.Local, the zone --since and --until read a date in, and
// the date render.SameLocalDay compares. A timestamp-less message gets "".
func dayKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	y, m, d := t.Local().Date()
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

// addSession counts one session's messages, parent and agents, each on its
// own day, and the session once on every day it has one on.
func (d dayTotals) addSession(messages []models.MessageAnalysis) {
	counted := map[string]bool{}
	for _, m := range messages {
		key := dayKey(m.Timestamp)
		day := d.day(key)
		if !counted[key] {
			counted[key] = true
			day.SessionCount++
		}
		day.MessageCount++
		day.TotalCost.Add(m.Cost)
	}
}

// addDays adds a set of days already totalled, a project's in a global
// report. Projects never share a session, so their session counts add.
func (d dayTotals) addDays(days []models.DayCost) {
	for _, src := range days {
		day := d.day(src.Date)
		day.SessionCount += src.SessionCount
		day.MessageCount += src.MessageCount
		day.TotalCost.Add(src.TotalCost)
	}
}

func (d dayTotals) day(key string) *models.DayCost {
	day, ok := d[key]
	if !ok {
		day = &models.DayCost{Date: key}
		d[key] = day
	}
	return day
}

// list is the days newest first, with the messages no day holds last. It's
// never nil, so json has a days array even when nothing is in it.
func (d dayTotals) list() []models.DayCost {
	days := make([]models.DayCost, 0, len(d))
	for _, day := range d {
		days = append(days, *day)
	}
	sort.Slice(days, func(i, j int) bool {
		if (days[i].Date == "") != (days[j].Date == "") {
			return days[j].Date == ""
		}
		return days[i].Date > days[j].Date
	})
	return days
}
