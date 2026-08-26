package devices

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func pairingAt(t *testing.T, start time.Time) (*Pairing, func(time.Duration)) {
	t.Helper()
	now := start
	pairing := &Pairing{Now: func() time.Time { return now }}
	return pairing, func(d time.Duration) { now = now.Add(d) }
}

func TestNothingPairsUntilSomebodyArmsIt(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())

	if pairing.Armed() {
		t.Fatal("pairing was armed before anyone asked")
	}
	// The whole point of arming is that the route does not exist the rest of
	// the time. A guessable code that is always live is not a small window.
	if err := pairing.Redeem("123456"); !errors.Is(err, ErrNotArmed) {
		t.Fatalf("err = %v, want ErrNotArmed", err)
	}
}

func TestArmingGivesASixDigitCode(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())

	code, expires, err := pairing.Arm()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Fatalf("code = %q, want six digits", code)
	}
	if !pairing.Armed() {
		t.Fatal("arming did not arm it")
	}
	if expires.IsZero() {
		t.Fatal("no expiry was reported")
	}
}

func TestTheRightCodePairsOnceAndOnlyOnce(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	code, _, _ := pairing.Arm()

	if err := pairing.Redeem(code); err != nil {
		t.Fatalf("the right code was refused: %v", err)
	}
	// Single use: a code that keeps working is a shared secret with a short
	// name.
	if pairing.Armed() {
		t.Fatal("pairing is still armed after being used")
	}
	if err := pairing.Redeem(code); !errors.Is(err, ErrNotArmed) {
		t.Fatalf("the code worked twice: %v", err)
	}
}

func TestAWrongCodeLeavesItArmedUntilTheCapIsReached(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	code, _, _ := pairing.Arm()

	for i := range maxPairingAttempts - 1 {
		if err := pairing.Redeem("000000"); !errors.Is(err, ErrWrongCode) {
			t.Fatalf("attempt %d: err = %v, want ErrWrongCode", i+1, err)
		}
		if !pairing.Armed() {
			t.Fatalf("attempt %d disarmed it early", i+1)
		}
	}
	// A typo should not cost someone the pairing session.
	if err := pairing.Redeem(code); err != nil {
		t.Fatalf("the right code was refused after typos: %v", err)
	}
}

func TestTooManyWrongCodesDisarmsIt(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	code, _, _ := pairing.Arm()

	for range maxPairingAttempts {
		_ = pairing.Redeem("000000")
	}

	// Five guesses at one of a million is what makes six digits safe. Without
	// the cap the code length would have to do that work instead.
	if pairing.Armed() {
		t.Fatal("pairing survived the attempt cap")
	}
	if err := pairing.Redeem(code); !errors.Is(err, ErrNotArmed) {
		t.Fatalf("the real code still worked after the cap: %v", err)
	}
}

func TestPairingExpires(t *testing.T) {
	pairing, advance := pairingAt(t, time.Now())
	code, _, _ := pairing.Arm()

	advance(pairingWindow + time.Second)

	if pairing.Armed() {
		t.Fatal("an expired pairing still reports itself armed")
	}
	if err := pairing.Redeem(code); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestReArmingReplacesTheOldCode(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	first, _, _ := pairing.Arm()
	second, _, _ := pairing.Arm()

	if first == second {
		t.Fatal("re-arming produced the same code")
	}
	if err := pairing.Redeem(first); !errors.Is(err, ErrWrongCode) {
		t.Fatalf("the superseded code was accepted: %v", err)
	}
	if err := pairing.Redeem(second); err != nil {
		t.Fatalf("the current code was refused: %v", err)
	}
}

func TestReArmingResetsTheAttemptCount(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	pairing.Arm()
	for range maxPairingAttempts {
		_ = pairing.Redeem("000000")
	}

	code, _, err := pairing.Arm()
	if err != nil {
		t.Fatal(err)
	}
	// Otherwise one burst of guesses locks pairing out permanently, and the
	// only fix is restarting the session.
	if err := pairing.Redeem(code); err != nil {
		t.Fatalf("a fresh pairing inherited the old attempt count: %v", err)
	}
}

func TestOnlyOneRacerPairs(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	code, _, _ := pairing.Arm()

	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := pairing.Redeem(code); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Single use has to survive two phones scanning at once.
	if won != 1 {
		t.Fatalf("%d callers paired, want exactly 1", won)
	}
}

func TestDisarmingStopsIt(t *testing.T) {
	pairing, _ := pairingAt(t, time.Now())
	code, _, _ := pairing.Arm()

	pairing.Disarm()

	if pairing.Armed() {
		t.Fatal("disarm did not disarm")
	}
	if err := pairing.Redeem(code); !errors.Is(err, ErrNotArmed) {
		t.Fatalf("err = %v, want ErrNotArmed", err)
	}
}
