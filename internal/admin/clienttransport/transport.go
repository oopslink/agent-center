// Package clienttransport builds *http.Transport instances that talk to
// the admin endpoint over either a unix socket or TLS with SSH-style
// fingerprint pinning. v2.3-7b (task #27) introduces this so CLI Client
// and worker-daemon AdminClient share one canonical dial path.
//
// The server side lives in internal/admin/api/tls.go (cert auto-gen +
// fingerprint format). The fingerprint format MUST match what
// api.FormatFingerprint emits (sha256:HH:HH:...:HH, 32 segments,
// uppercase).
package clienttransport

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Kind enumerates the supported admin transport modes.
type Kind int

const (
	KindUnknown Kind = iota
	KindUnix         // unix domain socket
	KindTCP          // TCP+TLS with fingerprint pinning
)

// Target captures the parsed admin endpoint location.
type Target struct {
	Kind Kind
	// Address is either a unix socket path (Kind=KindUnix) or a
	// "host:port" tcp address (Kind=KindTCP).
	Address string
}

// ParseTarget accepts:
//   - `unix:/path/to/admin.sock` → Kind=KindUnix
//   - `tcp://host:port`           → Kind=KindTCP
//   - bare path starting with `/` → KindUnix (legacy convenience for
//     existing operators who write socket paths directly)
//
// Returns an error on empty or unrecognised input.
func ParseTarget(spec string) (Target, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return Target{}, errors.New("clienttransport: empty admin target")
	}
	switch {
	case strings.HasPrefix(s, "unix:"):
		path := strings.TrimPrefix(s, "unix:")
		if path == "" {
			return Target{}, errors.New("clienttransport: unix target missing path")
		}
		return Target{Kind: KindUnix, Address: path}, nil
	case strings.HasPrefix(s, "tcp://"):
		addr := strings.TrimPrefix(s, "tcp://")
		if addr == "" || !strings.Contains(addr, ":") {
			return Target{}, errors.New("clienttransport: tcp target requires host:port")
		}
		return Target{Kind: KindTCP, Address: addr}, nil
	case strings.HasPrefix(s, "/"):
		return Target{Kind: KindUnix, Address: s}, nil
	default:
		return Target{}, fmt.Errorf("clienttransport: unrecognised admin target %q (use unix:/path or tcp://host:port)", s)
	}
}

// NewHTTPTransport returns an *http.Transport configured for the given
// target. For KindTCP, fingerprint must match exactly the format
// api.FormatFingerprint emits (sha256:HH:HH:...:HH, 32 segments,
// uppercase); empty fingerprint with KindTCP returns an error so
// callers can't accidentally ship "TLS without verify" to production.
//
// timeout is the per-request timeout for the wrapping *http.Client;
// callers wrap the returned *http.Transport themselves so they can
// also configure other client-level state (cookies etc. — n/a here).
func NewHTTPTransport(target Target, fingerprint string, timeout time.Duration) (*http.Transport, error) {
	switch target.Kind {
	case KindUnix:
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "unix", target.Address)
			},
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		}
		return tr, nil
	case KindTCP:
		fp := strings.TrimSpace(fingerprint)
		if fp == "" {
			return nil, errors.New("clienttransport: tcp target requires --server-fingerprint (pinning prevents MITM)")
		}
		if !looksLikeFingerprint(fp) {
			return nil, fmt.Errorf("clienttransport: fingerprint %q malformed (expected sha256:HH:HH:...:HH 32 segments)", fp)
		}
		tr := &http.Transport{
			DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialTLSPinned(ctx, target.Address, fp, timeout)
			},
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		}
		return tr, nil
	default:
		return nil, fmt.Errorf("clienttransport: unsupported target kind %d", target.Kind)
	}
}

// dialTLSPinned dials addr via TLS, then verifies the leaf cert's
// SHA256(DER) matches the expected fingerprint exactly. Fingerprint
// mismatch fails the connection.
//
// We use InsecureSkipVerify=true to bypass the standard PKI chain
// validation — the fingerprint IS our trust anchor. The pinning check
// below replaces "is this signed by a CA we trust" with "is this
// exactly the cert the operator pinned".
func dialTLSPinned(ctx context.Context, addr, expectedFingerprint string, timeout time.Duration) (net.Conn, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // fingerprint pinned below
			MinVersion:         tls.VersionTLS12,
		},
	}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("clienttransport: dial tls %s: %w", addr, err)
	}
	conn := rawConn.(*tls.Conn)
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("clienttransport: tls handshake to %s returned zero peer certs", addr)
	}
	gotFP := computeFingerprint(state.PeerCertificates[0].Raw)
	if !equalFingerprint(gotFP, expectedFingerprint) {
		_ = conn.Close()
		return nil, fmt.Errorf("clienttransport: fingerprint mismatch dialing %s: got %s, expected %s — the server cert changed (rotated?) OR you're being MITM'd; do NOT proceed until verified", addr, gotFP, expectedFingerprint)
	}
	return conn, nil
}

// computeFingerprint mirrors api.FormatFingerprint exactly. Duplicated
// here to avoid a cyclic dependency on the admin/api package (api is
// server-side; clienttransport must stay decoupled). Kept narrow + the
// shape test in tls_test.go on the server side guards the format
// contract across both copies.
func computeFingerprint(derBytes []byte) string {
	sum := sha256.Sum256(derBytes)
	parts := make([]string, 0, len(sum))
	for _, b := range sum {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return "sha256:" + strings.Join(parts, ":")
}

// equalFingerprint compares two fingerprint strings case-insensitively
// (operator might paste lowercase hex).
func equalFingerprint(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// looksLikeFingerprint validates the shape (sha256: prefix + 32 hex
// segments separated by colons). Doesn't check the bytes — that's
// the comparison step.
func looksLikeFingerprint(fp string) bool {
	fp = strings.TrimSpace(fp)
	if !strings.HasPrefix(fp, "sha256:") {
		return false
	}
	rest := strings.TrimPrefix(fp, "sha256:")
	parts := strings.Split(rest, ":")
	if len(parts) != 32 {
		return false
	}
	for _, p := range parts {
		if len(p) != 2 {
			return false
		}
		for _, c := range p {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// BaseURL returns the URL prefix appropriate to the target's transport.
// Used by client wrappers that build per-request URLs.
//
//   - unix → "http://unix" (the Host portion is a placeholder; the
//     Transport.DialContext ignores it)
//   - tcp  → "https://<addr>" (TLS is mandatory on tcp leg)
func (t Target) BaseURL() string {
	switch t.Kind {
	case KindUnix:
		return "http://unix"
	case KindTCP:
		return "https://" + t.Address
	default:
		return ""
	}
}
