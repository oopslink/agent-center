// tls.go — self-signed TLS cert auto-generation + fingerprint persistence
// for the admin TCP listener (v2.3-7a, task #27).
//
// Design choices (per @oopslink decision 2026-05-26):
//   - Self-signed cert + client fingerprint pinning (SSH-style trust),
//     NOT mTLS / NOT CA-signed. v3 candidates only.
//   - Cert: ECDSA P-256, 1-year validity, CN=agent-center,
//     SAN = 127.0.0.1 + all IPv4/IPv6 from net.Interfaces() + config hostname.
//   - Fingerprint: sha256(DER(cert)), uppercase hex with `:` every 2 bytes,
//     prefixed `sha256:` (SSH host-key style).
//   - Rotation = destructive: delete cert + key files, restart server,
//     new cert auto-generated. v2.3-7a does NOT support hot rotation.
//
// Failure modes (all observable + tested):
//   - cert exists but key missing → error; operator must delete both
//   - cert expired → error; same recovery
//   - parent dir not writable for fingerprint file → warning; cert still
//     loads, fingerprint also goes to stderr at boot
package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CertValidity is the auto-generated cert's validity window.
const CertValidity = 365 * 24 * time.Hour

// CertExpiryWarningWindow is the threshold below which the server emits
// an `admin.tcp_cert_expiring` observability event at boot.
const CertExpiryWarningWindow = 30 * 24 * time.Hour

// LoadOrGenerateCert ensures a usable TLS cert + key are at the given
// paths and returns the parsed cert along with the SSH-style fingerprint.
//
//   - If both files exist + cert is valid: load + return (generated=false).
//   - If both missing: generate fresh cert + key, write to disk, return
//     (generated=true).
//   - If cert exists but key missing (or vice versa): error. Operator
//     must delete both to regenerate (we never silently regenerate one
//     half — that would invalidate any client that pinned the existing
//     fingerprint).
//   - If cert exists but is expired: error. Same recovery.
//
// hostname is added to SAN (besides 127.0.0.1 + all interface IPs) so
// clients dialing by hostname can validate the cert. Empty hostname is
// allowed (skipped).
func LoadOrGenerateCert(certPath, keyPath, hostname string) (cert *tls.Certificate, fingerprint string, generated bool, err error) {
	certExists := fileExists(certPath)
	keyExists := fileExists(keyPath)

	switch {
	case certExists && keyExists:
		c, fp, lerr := loadCert(certPath, keyPath)
		if lerr != nil {
			return nil, "", false, lerr
		}
		return c, fp, false, nil
	case certExists && !keyExists:
		return nil, "", false, fmt.Errorf("admin tls: cert exists at %q but key missing at %q — delete both to regenerate", certPath, keyPath)
	case !certExists && keyExists:
		return nil, "", false, fmt.Errorf("admin tls: key exists at %q but cert missing at %q — delete both to regenerate", keyPath, certPath)
	}

	// Both missing — generate fresh.
	c, fp, gerr := generateAndPersist(certPath, keyPath, hostname)
	if gerr != nil {
		return nil, "", false, gerr
	}
	return c, fp, true, nil
}

// FormatFingerprint renders a SHA256 hash of DER-encoded cert bytes in
// SSH-style: `sha256:HH:HH:HH:...` (32 segments, uppercase hex).
//
// Operator-facing — must match the format clients will pin. Public so
// tests + future client-side pinning logic share one canonical formatter.
func FormatFingerprint(derBytes []byte) string {
	sum := sha256.Sum256(derBytes)
	parts := make([]string, 0, len(sum))
	for _, b := range sum {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return "sha256:" + strings.Join(parts, ":")
}

// WriteFingerprintFile writes the fingerprint string + newline to path
// with mode 0644 (operator-readable). Failure is non-fatal at the caller
// level — fingerprint also lands in stderr at boot.
func WriteFingerprintFile(path, fingerprint string) error {
	return os.WriteFile(path, []byte(fingerprint+"\n"), 0o644)
}

// CertExpiryWarning returns (true, daysRemaining) if the cert is within
// CertExpiryWarningWindow of expiry. Returns (false, days) otherwise.
// Caller uses this to decide whether to emit the
// `admin.tcp_cert_expiring` observability event at boot.
func CertExpiryWarning(cert *tls.Certificate) (warn bool, daysRemaining int) {
	if cert == nil || len(cert.Certificate) == 0 {
		return false, 0
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false, 0
	}
	remaining := time.Until(parsed.NotAfter)
	days := int(remaining / (24 * time.Hour))
	if remaining <= 0 {
		// Already expired — caller should have caught this in
		// loadCert, but be defensive.
		return true, days
	}
	if remaining < CertExpiryWarningWindow {
		return true, days
	}
	return false, days
}

// CertNotAfter returns the cert's NotAfter time. Used for boot banner.
// Returns zero time on parse error (caller treats as unknown).
func CertNotAfter(cert *tls.Certificate) time.Time {
	if cert == nil || len(cert.Certificate) == 0 {
		return time.Time{}
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return time.Time{}
	}
	return parsed.NotAfter
}

// --- internals -------------------------------------------------------

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func loadCert(certPath, keyPath string) (*tls.Certificate, string, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: load cert %q + key %q: %w", certPath, keyPath, err)
	}
	if len(cert.Certificate) == 0 {
		return nil, "", fmt.Errorf("admin tls: cert at %q has zero leaf bytes", certPath)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: parse cert at %q: %w", certPath, err)
	}
	if time.Now().After(parsed.NotAfter) {
		return nil, "", fmt.Errorf("admin tls: cert at %q expired %s — delete cert + key to regenerate", certPath, parsed.NotAfter.Format(time.RFC3339))
	}
	fp := FormatFingerprint(cert.Certificate[0])
	return &cert, fp, nil
}

func generateAndPersist(certPath, keyPath, hostname string) (*tls.Certificate, string, error) {
	// Make sure parent dirs exist.
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return nil, "", fmt.Errorf("admin tls: mkdir cert dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, "", fmt.Errorf("admin tls: mkdir key dir: %w", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: generate ec key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: generate serial: %w", err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "agent-center"},
		NotBefore:    now.Add(-1 * time.Minute), // small backdate for clock skew
		NotAfter:     now.Add(CertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	tmpl.IPAddresses, tmpl.DNSNames = buildSANs(hostname)

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: sign cert: %w", err)
	}

	// Write cert (PEM, mode 0644 — public key material).
	certPEM := &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(certPEM), 0o644); err != nil {
		return nil, "", fmt.Errorf("admin tls: write cert: %w", err)
	}

	// Write key (PEM, mode 0600 — private key).
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: marshal ec key: %w", err)
	}
	keyPEM := &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(keyPEM), 0o600); err != nil {
		return nil, "", fmt.Errorf("admin tls: write key: %w", err)
	}

	cert, err := tls.X509KeyPair(pem.EncodeToMemory(certPEM), pem.EncodeToMemory(keyPEM))
	if err != nil {
		return nil, "", fmt.Errorf("admin tls: reload generated pair: %w", err)
	}

	fp := FormatFingerprint(derBytes)
	return &cert, fp, nil
}

// buildSANs assembles the cert's SubjectAltName list. We always include
// 127.0.0.1, ::1, and every non-loopback unicast address on any host
// interface; plus the hostname (if non-empty) as a DNS name.
func buildSANs(hostname string) (ips []net.IP, dnsNames []string) {
	seen := map[string]bool{}
	add := func(ip net.IP) {
		s := ip.String()
		if seen[s] {
			return
		}
		seen[s] = true
		ips = append(ips, ip)
	}
	add(net.IPv4(127, 0, 0, 1))
	add(net.IPv6loopback)

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, _ := net.ParseCIDR(addr.String())
			if ip == nil {
				continue
			}
			if ip.IsLinkLocalUnicast() {
				continue
			}
			add(ip)
		}
	}

	if strings.TrimSpace(hostname) != "" {
		dnsNames = append(dnsNames, hostname)
	}
	// Always include the literal "localhost" DNS name so clients
	// dialing localhost get a valid SAN.
	dnsNames = append(dnsNames, "localhost")
	return ips, dnsNames
}

