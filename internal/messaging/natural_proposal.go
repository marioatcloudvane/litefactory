package messaging

import "time"

// PlainWindow spells out an RFC3339 window in local time: whole days as
// "Mon 12 – Sun 18 Oct", other bounds with the time of day, and a window
// ending about now as "since …".
func PlainWindow(after, before string, now time.Time) string {
	from, e1 := time.Parse(time.RFC3339, after)
	to, e2 := time.Parse(time.RFC3339, before)
	if e1 != nil || e2 != nil {
		return after + " to " + before
	}
	from, to = from.In(time.Local), to.In(time.Local)
	midnight := func(t time.Time) bool { return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 }
	if midnight(from) && midnight(to) {
		last := to.AddDate(0, 0, -1)
		switch {
		case !last.After(from):
			return from.Format("Mon 2 Jan")
		case from.Month() == last.Month() && from.Year() == last.Year():
			return from.Format("Mon 2") + " – " + last.Format("Mon 2 Jan")
		}
		return from.Format("Mon 2 Jan") + " – " + last.Format("Mon 2 Jan")
	}
	if d := now.Sub(to); d > -time.Minute && d < 5*time.Minute {
		return "since " + from.Format("Mon 2 Jan 15:04")
	}
	return from.Format("Mon 2 Jan 15:04") + " – " + to.Format("Mon 2 Jan 15:04")
}
