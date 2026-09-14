// Package quiet evaluates schedule windows ("quiet hours") that pause
// downloads or processing, or tighten throttling, at certain times.
package quiet

import (
	"slices"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/settings"
)

// Effects are the combined effects of the windows active at a moment.
type Effects struct {
	PauseDownloads  bool     `json:"pauseDownloads"`
	PauseProcessing bool     `json:"pauseProcessing"`
	Throttle        string   `json:"throttle,omitempty"`
	Windows         []string `json:"windows,omitempty"`
}

var days = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Validate checks a schedule.
func Validate(s settings.Schedule) error { return s.Validate() }

// strength orders throttle presets; the gentlest active one wins.
var strength = map[string]int{"fast": 1, "normal": 2, "gentle": 3}

// Evaluate returns the effects of the windows active at now.
func Evaluate(s settings.Schedule, now time.Time) Effects {
	loc := time.Local
	if s.Timezone != "" {
		if l, err := time.LoadLocation(s.Timezone); err == nil {
			loc = l
		}
	}
	t := now.In(loc)
	minute := t.Hour()*60 + t.Minute()
	today := days[t.Weekday()]
	yesterday := days[(int(t.Weekday())+6)%7]
	var e Effects
	for _, w := range s.Windows {
		start, err1 := settings.ParseClock(w.Start)
		end, err2 := settings.ParseClock(w.End)
		if err1 != nil || err2 != nil || start == end {
			continue
		}
		on := func(day string) bool {
			return len(w.Days) == 0 || slices.ContainsFunc(w.Days, func(d string) bool { return strings.EqualFold(d, day) })
		}
		active := false
		if start < end {
			active = on(today) && minute >= start && minute < end
		} else { // crosses midnight: the part after midnight belongs to the previous day
			active = (on(today) && minute >= start) || (on(yesterday) && minute < end)
		}
		if !active {
			continue
		}
		e.PauseDownloads = e.PauseDownloads || w.PauseDownloads
		e.PauseProcessing = e.PauseProcessing || w.PauseProcessing
		if strength[w.Throttle] > strength[e.Throttle] {
			e.Throttle = w.Throttle
		}
		name := w.Name
		if name == "" {
			name = w.Start + "–" + w.End
		}
		e.Windows = append(e.Windows, name)
	}
	return e
}
