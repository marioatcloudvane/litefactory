package owneractions

import (
	"strconv"
	"strings"
	"time"
)

// RelativeBound turns a spanned relative time phrase into RFC3339 local-midnight
// bounds of now's location. A closed period without "since" also returns its end;
// unrecognised text is not ok and stays for meterview to reject.
func RelativeBound(phrase string, now time.Time) (string, string, bool) {
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	since := strings.HasPrefix(phrase, "since ")
	phrase = strings.TrimSpace(strings.TrimPrefix(phrase, "since "))
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	var from, to time.Time
	switch phrase {
	case "today":
		from = day
	case "yesterday":
		from, to = day.AddDate(0, 0, -1), day
	case "this week":
		from = week
	case "last week":
		from, to = week.AddDate(0, 0, -7), week
	case "this month":
		from = month
	case "last month":
		from, to = month.AddDate(0, -1, 0), month
	default:
		found := false
		for d := time.Sunday; d <= time.Saturday; d++ {
			if phrase == strings.ToLower(d.String()) {
				from, found = day.AddDate(0, 0, -(int(day.Weekday())-int(d)+7)%7), true
			}
		}
		if !found {
			return "", "", false
		}
	}
	if since || to.IsZero() {
		return from.Format(time.RFC3339), "", true
	}
	return from.Format(time.RFC3339), to.Format(time.RFC3339), true
}

// AnalysisWindowRefusal names every window form AnalysisWindow accepts.
const AnalysisWindowRefusal = "Please give the analysis window as today, today since 2 pm, since 14:00, since 2 pm yesterday, yesterday, last N days, last N hours, this week, last week, or as exact after and before times."

// AnalysisWindow turns a spanned window phrase into closed UTC bounds: a clock
// time ("today since 2 pm", "since 14:00 yesterday") and "last N days" (or
// hours) end now, and the calendar phrases of RelativeBound end at their
// period's end or now. A trailing "only" ("today only") is ignored.
func AnalysisWindow(phrase string, now time.Time) (string, string, bool) {
	text := strings.ToLower(strings.TrimSpace(phrase))
	text = strings.TrimSpace(strings.TrimSuffix(text, " only"))
	if start, matched := ClockWindowStart(text, now.In(time.Local)); matched {
		if start.After(now) {
			return "", "", false
		}
		return start.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339), true
	}
	fields := strings.Fields(strings.TrimPrefix(strings.TrimPrefix(text, "last "), "past "))
	if len(fields) == 2 {
		n, err := strconv.Atoi(fields[0])
		unit := map[string]time.Duration{"day": 24 * time.Hour, "hour": time.Hour}[strings.TrimSuffix(fields[1], "s")]
		if err == nil && n > 0 && n <= 366 && unit > 0 {
			return now.UTC().Add(-time.Duration(n) * unit).Format(time.RFC3339), now.UTC().Format(time.RFC3339), true
		}
	}
	from, to, ok := RelativeBound(text, now.In(time.Local))
	if !ok {
		return "", "", false
	}
	start, _ := time.Parse(time.RFC3339, from)
	end := now
	if to != "" {
		end, _ = time.Parse(time.RFC3339, to)
	}
	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), true
}

// ClockWindowStart reads "since"/"from" and a clock time ("14:00", "14", "2 pm",
// "2pm", "2:30 pm"), optionally preceded or followed by "today" or "yesterday",
// as that local time on the chosen day (today by default).
func ClockWindowStart(text string, now time.Time) (time.Time, bool) {
	fields := strings.Fields(strings.ReplaceAll(text, ",", " "))
	days := 0
	if len(fields) > 0 && (fields[0] == "today" || fields[0] == "yesterday") {
		days, fields = map[string]int{"today": 0, "yesterday": 1}[fields[0]], fields[1:]
	} else if n := len(fields); n > 0 && (fields[n-1] == "today" || fields[n-1] == "yesterday") {
		days, fields = map[string]int{"today": 0, "yesterday": 1}[fields[n-1]], fields[:n-1]
	}
	if len(fields) < 2 || len(fields) > 3 || fields[0] != "since" && fields[0] != "from" {
		return time.Time{}, false
	}
	clock := strings.Join(fields[1:], "")
	meridiem := ""
	if strings.HasSuffix(clock, "am") || strings.HasSuffix(clock, "pm") {
		clock, meridiem = clock[:len(clock)-2], clock[len(clock)-2:]
	}
	hourText, minuteText, hasMinutes := strings.Cut(clock, ":")
	hour, err := strconv.Atoi(hourText)
	if err != nil || len(hourText) > 2 || hour < 0 || hour > 23 {
		return time.Time{}, false
	}
	minute := 0
	if hasMinutes {
		if minute, err = strconv.Atoi(minuteText); err != nil || len(minuteText) != 2 || minute < 0 || minute > 59 {
			return time.Time{}, false
		}
	}
	if meridiem != "" {
		if hour < 1 || hour > 12 {
			return time.Time{}, false
		}
		hour %= 12
		if meridiem == "pm" {
			hour += 12
		}
	}
	return time.Date(now.Year(), now.Month(), now.Day()-days, hour, minute, 0, 0, now.Location()), true
}
