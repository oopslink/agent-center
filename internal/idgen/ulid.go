// Package idgen provides thread-safe ULID generation backed by oklog/ulid/v2.
//
// Per 02-persistence-schema § 2: 26-char Crockford Base32 ULID with
// MonotonicEntropy so ids generated in the same millisecond stay strictly
// ordered.
package idgen

import (
	"crypto/rand"
	"io"
	"math"
	mrand "math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/oopslink/agent-center/internal/clock"
)

// Generator emits unique, monotonic ULID strings.
type Generator interface {
	NewULID() string
}

// ulidGen is the default Generator implementation; concurrency-safe.
type ulidGen struct {
	mu      sync.Mutex
	clock   clock.Clock
	entropy io.Reader
}

// NewGenerator returns a thread-safe ULID generator that uses the given clock
// for timestamps. The default reader is crypto/rand wrapped in MonotonicReader
// so same-millisecond IDs strictly increment.
func NewGenerator(c clock.Clock) Generator {
	if c == nil {
		c = clock.SystemClock{}
	}
	mono := ulid.Monotonic(rand.Reader, 0)
	return &ulidGen{clock: c, entropy: mono}
}

// NewGeneratorWithReader is used by tests to inject a deterministic entropy
// source.
func NewGeneratorWithReader(c clock.Clock, r io.Reader) Generator {
	if c == nil {
		c = clock.SystemClock{}
	}
	return &ulidGen{clock: c, entropy: ulid.Monotonic(r, 0)}
}

// NewULID returns a fresh ULID string (Crockford Base32, 26 chars).
func (g *ulidGen) NewULID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	t := g.clock.Now()
	ms := ulid.Timestamp(t)
	id := ulid.MustNew(ms, g.entropy)
	return id.String()
}

// DeterministicReader returns a math/rand-backed io.Reader seeded with seed.
// Tests only; never use for production IDs.
func DeterministicReader(seed int64) io.Reader {
	src := mrand.NewSource(seed)
	return &deterministicReader{r: mrand.New(src)}
}

type deterministicReader struct {
	r *mrand.Rand
}

func (d *deterministicReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(d.r.Intn(math.MaxUint8 + 1))
	}
	return len(p), nil
}

// MustNewULID is a convenience for one-off ID generation backed by the system
// clock and crypto/rand. Avoid in services; inject a Generator instead.
func MustNewULID() string {
	return NewGenerator(clock.SystemClock{}).NewULID()
}

// IsValid reports whether s parses as a ULID.
func IsValid(s string) bool {
	_, err := ulid.Parse(s)
	return err == nil
}

// Time extracts the timestamp from a ULID string; second return reports parse
// success.
func Time(s string) (time.Time, bool) {
	id, err := ulid.Parse(s)
	if err != nil {
		return time.Time{}, false
	}
	return ulid.Time(id.Time()), true
}
