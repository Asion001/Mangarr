package quiet

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/settings"
)

func TestEvaluate(t *testing.T) {
	s := settings.Schedule{Timezone: "Europe/Madrid", Windows: []settings.ScheduleWindow{
		{Name: "night", Start: "22:00", End: "07:00", Days: []string{"fri"}, PauseDownloads: true, Throttle: "normal"},
		{Name: "work", Start: "09:00", End: "17:00", Days: []string{"mon", "tue", "wed", "thu", "fri"}, PauseProcessing: true, Throttle: "gentle"},
		{Name: "broken", Start: "25:00", End: "01:00", PauseDownloads: true},
	}}
	if err := Validate(s); err == nil {
		t.Fatal("25:00 must be invalid")
	}
	madrid, _ := time.LoadLocation("Europe/Madrid")
	at := func(y, m, d, h, min int) time.Time { return time.Date(y, time.Month(m), d, h, min, 0, 0, madrid) }
	// 2026-09-11 is a Friday
	cases := []struct {
		t        time.Time
		dl, proc bool
		throttle string
	}{
		{at(2026, 9, 11, 23, 30), true, false, "normal"}, // Friday night
		{at(2026, 9, 12, 6, 59), true, false, "normal"},  // Saturday morning, still Friday's window
		{at(2026, 9, 12, 7, 0), false, false, ""},        // window ended
		{at(2026, 9, 13, 23, 30), false, false, ""},      // Sunday night: not a Friday window
		{at(2026, 9, 11, 10, 0), false, true, "gentle"},  // Friday at work
		{at(2026, 9, 12, 10, 0), false, false, ""},       // Saturday
	}
	for _, c := range cases {
		e := Evaluate(s, c.t.UTC())
		if e.PauseDownloads != c.dl || e.PauseProcessing != c.proc || e.Throttle != c.throttle {
			t.Errorf("%s: got %+v", c.t, e)
		}
	}
	// gentlest throttle wins when windows overlap
	s.Windows = append(s.Windows, settings.ScheduleWindow{Start: "00:00", End: "24:00", Throttle: "fast"})
	if e := Evaluate(s, at(2026, 9, 11, 10, 0).UTC()); e.Throttle != "gentle" || len(e.Windows) != 2 {
		t.Fatalf("overlap: %+v", e)
	}
}
