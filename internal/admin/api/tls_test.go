package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// eccurve returns P-256 so writeExpiredCertPair can build a key. Mirrors
// tls.go's generateAndPersist.
func eccurve() elliptic.Curve { return elliptic.P256() }

// v2.3-7a (task #27): tests for cert auto-gen + fingerprint + load/regen
// behaviour. Production code lives in tls.go.

func TestLoadOrGenerateCert_FreshGenerates(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "admin-tls.crt")
	keyPath := filepath.Join(dir, "admin-tls.key")

	cert, fp, gen, err := LoadOrGenerateCert(certPath, keyPath, "test-host")
	if err != nil {
		t.Fatalf("LoadOrGenerateCert: %v", err)
	}
	if !gen {
		t.Fatalf("expected gen=true on first call")
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("cert nil/empty")
	}
	if !strings.HasPrefix(fp, "sha256:") {
		t.Fatalf("fingerprint missing prefix: %q", fp)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert file missing: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file missing: %v", err)
	}
}

func TestLoadOrGenerateCert_LoadStable(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "admin-tls.crt")
	keyPath := filepath.Join(dir, "admin-tls.key")

	_, fp1, gen1, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !gen1 {
		t.Fatalf("first call should generate")
	}

	// Second call with same paths must load + return same fingerprint.
	_, fp2, gen2, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if gen2 {
		t.Fatalf("second call should NOT regenerate")
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint changed: %q vs %q", fp1, fp2)
	}
}

func TestLoadOrGenerateCert_CertExistsKeyMissing(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "admin-tls.crt")
	keyPath := filepath.Join(dir, "admin-tls.key")

	// Generate then delete the key half only.
	if _, _, _, err := LoadOrGenerateCert(certPath, keyPath, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("rm key: %v", err)
	}

	_, _, _, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err == nil || !strings.Contains(err.Error(), "key missing") {
		t.Fatalf("expected key-missing error, got %v", err)
	}
}

func TestLoadOrGenerateCert_KeyExistsCertMissing(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "admin-tls.crt")
	keyPath := filepath.Join(dir, "admin-tls.key")

	if _, _, _, err := LoadOrGenerateCert(certPath, keyPath, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Remove(certPath); err != nil {
		t.Fatalf("rm cert: %v", err)
	}

	_, _, _, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err == nil || !strings.Contains(err.Error(), "cert missing") {
		t.Fatalf("expected cert-missing error, got %v", err)
	}
}

func TestLoadOrGenerateCert_Expired(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "admin-tls.crt")
	keyPath := filepath.Join(dir, "admin-tls.key")

	// Use an internal helper to write an expired cert. We re-use the
	// same key gen path as generateAndPersist but with NotAfter in the
	// past.
	if err := writeExpiredCertPair(t, certPath, keyPath); err != nil {
		t.Fatalf("seed expired: %v", err)
	}

	_, _, _, err := LoadOrGenerateCert(certPath, keyPath, "")
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestFormatFingerprint_Shape(t *testing.T) {
	// 32-byte DER → 32 hex pairs joined by ":".
	der := make([]byte, 100)
	for i := range der {
		der[i] = byte(i)
	}
	fp := FormatFingerprint(der)
	re := regexp.MustCompile(`^sha256:[0-9A-F]{2}(:[0-9A-F]{2}){31}$`)
	if !re.MatchString(fp) {
		t.Fatalf("fingerprint shape mismatch: %q", fp)
	}
}

func TestWriteFingerprintFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fp")
	want := "sha256:AA:BB:CC"
	if err := WriteFingerprintFile(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != want {
		t.Fatalf("want %q got %q", want, got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("perm = %v, want 0644", info.Mode().Perm())
	}
}

func TestCertExpiryWarning_NoWarn(t *testing.T) {
	dir := t.TempDir()
	cert, _, _, err := LoadOrGenerateCert(
		filepath.Join(dir, "c"), filepath.Join(dir, "k"), "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	warn, days := CertExpiryWarning(cert)
	if warn {
		t.Fatalf("fresh cert should not warn, days=%d", days)
	}
	if days < 360 {
		t.Fatalf("fresh cert should have ~365 days, got %d", days)
	}
}

func TestCertNotAfter_ReturnsValid(t *testing.T) {
	dir := t.TempDir()
	cert, _, _, err := LoadOrGenerateCert(
		filepath.Join(dir, "c"), filepath.Join(dir, "k"), "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	na := CertNotAfter(cert)
	if na.IsZero() {
		t.Fatal("NotAfter zero")
	}
	if !na.After(time.Now()) {
		t.Fatalf("NotAfter should be future, got %v", na)
	}
}

func TestCertSAN_IncludesLoopback(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c")
	keyPath := filepath.Join(dir, "k")
	cert, _, _, err := LoadOrGenerateCert(certPath, keyPath, "my-host")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	gotLoopback := false
	for _, ip := range parsed.IPAddresses {
		if ip.IsLoopback() {
			gotLoopback = true
			break
		}
	}
	if !gotLoopback {
		t.Fatalf("SAN missing loopback IP: ips=%v", parsed.IPAddresses)
	}
	gotHostname := false
	gotLocalhost := false
	for _, dns := range parsed.DNSNames {
		if dns == "my-host" {
			gotHostname = true
		}
		if dns == "localhost" {
			gotLocalhost = true
		}
	}
	if !gotHostname {
		t.Fatalf("SAN missing hostname 'my-host': dns=%v", parsed.DNSNames)
	}
	if !gotLocalhost {
		t.Fatalf("SAN missing 'localhost': dns=%v", parsed.DNSNames)
	}
}

// --- helpers ---------------------------------------------------------

// writeExpiredCertPair writes a self-signed cert + key with NotAfter in
// the past, so we can assert LoadOrGenerateCert rejects expired certs.
func writeExpiredCertPair(t *testing.T, certPath, keyPath string) error {
	t.Helper()
	priv, err := ecdsa.GenerateKey(eccurve(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "expired-test"},
		NotBefore:    time.Now().Add(-2 * 365 * 24 * time.Hour),
		NotAfter:     time.Now().Add(-1 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	certPEM := &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(certPEM), 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(keyPEM), 0o600); err != nil {
		return err
	}
	return nil
}

// Ensure tests compile with all imports used.
var _ = pkix.Name{}
var _ = tls.Certificate{}
