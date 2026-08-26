// Package devices records which devices may reach a running Kolkrabbi.
//
// One token per device rather than one per machine: losing a phone should cost
// that phone its access and nothing else. Tokens are stored as hashes, the way
// a password file works, so a device file that leaks is an inconvenience rather
// than a compromise.
package devices

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/onembyte/kolkrabbi/internal/atomicfile"
)

// Tier is how much a paired device may do.
//
// Reading and steering are separated because they are different risks: a device
// that can watch a session is not automatically one that should be able to
// approve what the session does next.
type Tier string

const (
	// TierRead can watch: the event stream, the transcript, the cost.
	TierRead Tier = "read"
	// TierSteer can also answer permission prompts, send turns and interrupt.
	TierSteer Tier = "steer"
)

// tokenBytes is the entropy in an issued token. 32 bytes is past the point
// where guessing is the attack anyone would choose.
const tokenBytes = 32

// Device is one paired device. It never holds the token, only its hash.
type Device struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Tier     Tier      `json:"tier"`
	Hash     string    `json:"hash"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

// Store is the set of paired devices.
//
// Safe for concurrent use: every HTTP request authenticates, and
// authenticating writes a last-seen time, so two devices talking at once is
// the normal case for a server rather than an edge one.
type Store struct {
	mu      sync.Mutex
	devices []Device
	// Now is the clock, replaceable so a test can assert on last-seen times
	// without sleeping.
	Now func() time.Time
}

type deviceFile struct {
	Devices []Device `json:"devices"`
}

// Load reads a device file. A missing file is not an error: no paired devices
// is the normal starting state.
func Load(file string) (*Store, error) {
	store := &Store{Now: time.Now}
	body, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var stored deviceFile
	if err := json.Unmarshal(body, &stored); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	store.devices = stored.Devices
	return store, nil
}

// Save writes the device file.
func (s *Store) Save(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	s.mu.Lock()
	body, err := json.MarshalIndent(deviceFile{Devices: s.devices}, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// 0600: a file that grants access to a running agent.
	return atomicfile.Write(file, append(body, '\n'), 0o600)
}

// Add pairs a device and returns it with its token.
//
// The token is returned once and never again — it is not stored, only its
// hash — so a caller that loses it has to pair the device again. That is the
// property that makes a stolen device file useless.
func (s *Store) Add(label string, tier Tier) (Device, string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Device{}, "", fmt.Errorf("generating a device token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return Device{}, "", fmt.Errorf("generating a device id: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	device := Device{
		ID:      "dev_" + hex.EncodeToString(id),
		Label:   label,
		Tier:    tier,
		Hash:    hashToken(token),
		Created: s.now(),
	}
	s.devices = append(s.devices, device)
	return device, token, nil
}

// Authenticate finds the device a token belongs to, and records that it was
// used.
//
// "Which of these is still in use" is the question someone asks before
// revoking, and a list that cannot answer it invites revoking the wrong one.
func (s *Store) Authenticate(token string) (Device, bool) {
	if token == "" {
		return Device{}, false
	}
	want := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		// Constant time: the comparison is against a hash, but a length-and-
		// prefix oracle over a set of devices is still an oracle.
		if subtle.ConstantTimeCompare([]byte(s.devices[i].Hash), []byte(want)) == 1 {
			s.devices[i].LastSeen = s.now()
			return s.devices[i], true
		}
	}
	return Device{}, false
}

// Revoke removes one device, reporting whether it was there.
func (s *Store) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].ID == id {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			return true
		}
	}
	return false
}

// List returns the paired devices, in the order they were paired.
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, len(s.devices))
	copy(out, s.devices)
	return out
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
