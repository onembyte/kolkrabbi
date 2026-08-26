package devices

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// pairingWindow is how long a code lives. Long enough to walk to the other
// device, short enough that nobody leaves one armed by accident.
const pairingWindow = 2 * time.Minute

// maxPairingAttempts is what makes six digits safe.
//
// Five guesses at one of a million, inside a window a person is watching, is a
// worse bet than any other way in. Without the cap the code's length would have
// to do that work, and a code long enough to resist guessing is a code nobody
// wants to type.
const maxPairingAttempts = 5

// Errors a redemption can produce. They are distinct because the caller shows
// different things: a wrong code invites another try, an expired or unarmed one
// invites arming again.
var (
	ErrNotArmed  = errors.New("pairing is not open")
	ErrExpired   = errors.New("the pairing code has expired")
	ErrWrongCode = errors.New("that pairing code is not right")
)

// Pairing is the short window during which a new device may be added.
//
// It is armed deliberately and briefly rather than being always available. The
// endpoint that redeems a code cannot require a credential — that is what
// pairing is for — so the thing that keeps it safe is that most of the time it
// does not exist.
type Pairing struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	armed    bool

	// Now is the clock, replaceable so a test can expire a window without
	// waiting two minutes for it.
	Now func() time.Time
}

// Arm opens a pairing window, replacing any window already open.
func (p *Pairing) Arm() (code string, expires time.Time, err error) {
	code, err = newPairingCode()
	if err != nil {
		return "", time.Time{}, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.code = code
	p.expires = p.now().Add(pairingWindow)
	// Reset, or one burst of guesses would lock pairing out permanently and
	// the only fix would be restarting the session.
	p.attempts = 0
	p.armed = true
	return code, p.expires, nil
}

// Armed reports whether a code can currently be redeemed.
func (p *Pairing) Armed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.liveLocked()
}

// Redeem consumes the pairing window if the code is right.
//
// Success disarms: a code that keeps working is a shared secret with a short
// name. Failure disarms only once the attempt cap is reached, so a typo does
// not cost someone their pairing session.
func (p *Pairing) Redeem(code string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.armed {
		return ErrNotArmed
	}
	if !p.now().Before(p.expires) {
		p.armed = false
		return ErrExpired
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		p.attempts++
		if p.attempts >= maxPairingAttempts {
			p.armed = false
		}
		return ErrWrongCode
	}

	p.armed = false
	return nil
}

// Disarm closes the window.
func (p *Pairing) Disarm() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.armed = false
}

// liveLocked reports whether the window is open and unexpired.
func (p *Pairing) liveLocked() bool {
	return p.armed && p.now().Before(p.expires)
}

func (p *Pairing) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// newPairingCode returns six digits from a source worth trusting.
//
// math/rand would be fine against a person and useless against anyone who
// knows when the session started.
func newPairingCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generating a pairing code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
