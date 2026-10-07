package continuity

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the zones below exist on every test machine

	"github.com/onembyte/kolkrabbi/internal/provider"
)

// A vendor's reset is trusted only while it is still ahead. One already past
// (a stale window, a skewed clock) would make the monitor retry at once, meet
// the same limit with the same stale time, and spin; kolk estimates instead.
func TestAPauseTrustsOnlyAResetStillAhead(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	allowance := provider.Limit{Kind: provider.LimitSubscriptionAllowance, Scope: provider.ScopeAccount}

	ahead := allowance
	ahead.ResetAt = now.Add(72 * time.Hour)
	if p := PauseFor(ahead, "", now); p.Estimated || !p.ResetAt.Equal(ahead.ResetAt) {
		t.Fatalf("a reset still ahead = %+v, want the vendor's time", p)
	}
	for name, reset := range map[string]time.Time{"past": now.Add(-time.Minute), "now": now} {
		stale := allowance
		stale.ResetAt = reset
		if p := PauseFor(stale, "", now); !p.Estimated || !p.ResetAt.Equal(now.Add(15*time.Minute)) {
			t.Errorf("a reset at %s = %+v, want kolk's 15-minute estimate", name, p)
		}
		stale.RetryAfter = 10 * time.Minute
		if p := PauseFor(stale, "", now); p.Estimated || !p.ResetAt.Equal(now.Add(10*time.Minute)) {
			t.Errorf("a reset at %s with Retry-After = %+v, want the Retry-After", name, p)
		}
	}
}

// A vendor's reset can be days away: the seven-day window. "reset at 09:00"
// would not say which day, so a time that is not today names its day.
func TestAResetNamesItsDayWhenItIsNotToday(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.Local) // a Monday
	cases := []struct {
		reset time.Time
		want  string
	}{
		{time.Date(2026, 9, 28, 12, 0, 0, 0, time.Local), "12:00"},
		{time.Date(2026, 9, 29, 0, 10, 0, 0, time.Local), "Tue 00:10"},
		{time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local), "Wed 09:00"},
		{time.Date(2026, 10, 4, 23, 0, 0, 0, time.Local), "Sun 23:00"},
		{time.Date(2026, 10, 5, 9, 0, 0, 0, time.Local), "Oct 5 09:00"},
		{time.Date(2026, 10, 9, 9, 0, 0, 0, time.Local), "Oct 9 09:00"},
	}
	for _, c := range cases {
		if got := ResetClock(c.reset, now, time.Local); got != c.want {
			t.Errorf("reset %v reads %q, want %q", c.reset, got, c.want)
		}
	}
}

// Days are counted on calendar dates, not in 24-hour steps: the night the
// clocks spring forward is 23 hours long, and tomorrow is still tomorrow.
func TestAResetCountsDaysAcrossADaylightSavingChange(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, newYork) // Sunday; clocks sprang forward at 02:00
	for reset, want := range map[time.Time]string{
		time.Date(2026, 3, 8, 23, 0, 0, 0, newYork): "23:00",
		time.Date(2026, 3, 9, 9, 0, 0, 0, newYork):  "Mon 09:00",
		time.Date(2026, 3, 15, 1, 0, 0, 0, newYork): "Mar 15 01:00",
	} {
		if got := ResetClock(reset, now, newYork); got != want {
			t.Errorf("reset %v reads %q, want %q", reset, got, want)
		}
	}
	// The reader's zone decides the day: the same instant is Monday in New
	// York and already Tuesday in Tokyo.
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if got := ResetClock(time.Date(2026, 3, 9, 23, 0, 0, 0, newYork), now, tokyo); got != "Tue 12:00" {
		t.Errorf("read in Tokyo = %q, want Tue 12:00", got)
	}
}

// Every place a pause shows its time reads it through Resumes, so the day is
// named there too: in the status, the pause error and the monitor's notice.
func TestAPauseDaysAwayNamesItsDayWhereverItIsShown(t *testing.T) {
	p := Pause{ResetAt: time.Now().Add(72 * time.Hour)}
	day := strings.Fields(p.Resumes())[0]
	if _, err := time.Parse("Mon", day); err != nil {
		t.Fatalf("Resumes() = %q, want a weekday first for a reset three days away", p.Resumes())
	}
	if got := p.RetryStatus(); !strings.HasPrefix(got, "reset at "+day+" ") {
		t.Fatalf("RetryStatus() = %q, want the day named", got)
	}
}
