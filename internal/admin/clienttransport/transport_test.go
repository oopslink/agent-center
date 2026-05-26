package clienttransport

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseTarget_Unix(t *testing.T) {
	tg, err := ParseTarget("unix:/var/run/admin.sock")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Kind != KindUnix || tg.Address != "/var/run/admin.sock" {
		t.Fatalf("unexpected: %+v", tg)
	}
}

func TestParseTarget_TCP(t *testing.T) {
	tg, err := ParseTarget("tcp://host.example:7300")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Kind != KindTCP || tg.Address != "host.example:7300" {
		t.Fatalf("unexpected: %+v", tg)
	}
}

func TestParseTarget_BarePathFallback(t *testing.T) {
	tg, err := ParseTarget("/tmp/admin.sock")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Kind != KindUnix || tg.Address != "/tmp/admin.sock" {
		t.Fatalf("unexpected: %+v", tg)
	}
}

func TestParseTarget_Errors(t *testing.T) {
	cases := []string{
		"",
		"http://host:7300",
		"tcp://",
		"tcp://host", // missing port
		"unix:",
	}
	for _, c := range cases {
		if _, err := ParseTarget(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestNewHTTPTransport_TCPRequiresFingerprint(t *testing.T) {
	tg := Target{Kind: KindTCP, Address: "host:7300"}
	if _, err := NewHTTPTransport(tg, "", time.Second); err == nil {
		t.Fatal("expected error on empty fingerprint")
	}
}

func TestNewHTTPTransport_TCPRequiresValidFingerprint(t *testing.T) {
	tg := Target{Kind: KindTCP, Address: "host:7300"}
	cases := []string{
		"sha256:foo",
		"AA:BB:CC", // missing prefix
		"sha256:GG:HH:II:JJ:KK:LL:MM:NN:OO:PP:QQ:RR:SS:TT:UU:VV:WW:XX:YY:ZZ:00:11:22:33:44:55:66:77:88:99:AA:BB",
	}
	for _, fp := range cases {
		if _, err := NewHTTPTransport(tg, fp, time.Second); err == nil {
			t.Errorf("expected error for fingerprint %q", fp)
		}
	}
}

func TestNewHTTPTransport_FingerprintAcceptedFormat(t *testing.T) {
	tg := Target{Kind: KindTCP, Address: "host:7300"}
	fp := "sha256:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF"
	if _, err := NewHTTPTransport(tg, fp, time.Second); err != nil {
		t.Fatalf("good fingerprint rejected: %v", err)
	}
}

func TestTarget_BaseURL(t *testing.T) {
	if got := (Target{Kind: KindUnix, Address: "/x"}).BaseURL(); got != "http://unix" {
		t.Errorf("unix base url = %q", got)
	}
	if got := (Target{Kind: KindTCP, Address: "h:7"}).BaseURL(); got != "https://h:7" {
		t.Errorf("tcp base url = %q", got)
	}
}

// TestTLSPinning_RoundTrip exercises the actual TLS dial path against a
// httptest TLS server. We pin the server's actual cert fingerprint and
// confirm requests succeed.
func TestTLSPinning_RoundTrip(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// httptest's TLS server uses an auto-generated cert; grab it.
	cert := srv.Certificate()
	fp := computeFingerprint(cert.Raw)
	addr := strings.TrimPrefix(srv.URL, "https://")

	tg := Target{Kind: KindTCP, Address: addr}
	tr, err := NewHTTPTransport(tg, fp, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cli := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	resp, err := cli.Get(tg.BaseURL() + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

// TestTLSPinning_MismatchFails verifies that a wrong fingerprint causes
// the dial to fail (this is the MITM defense).
func TestTLSPinning_MismatchFails(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "https://")

	// Pin a wrong fingerprint (all zeros) — different from real cert.
	wrongFP := "sha256:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00"

	tg := Target{Kind: KindTCP, Address: addr}
	tr, err := NewHTTPTransport(tg, wrongFP, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cli := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	_, err = cli.Get(tg.BaseURL() + "/")
	if err == nil {
		t.Fatal("expected error on fingerprint mismatch")
	}
	if !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("expected fingerprint mismatch error, got: %v", err)
	}
}

// Ensure crypto imports we use show up.
var _ = tls.Certificate{}
var _ = x509.Certificate{}
