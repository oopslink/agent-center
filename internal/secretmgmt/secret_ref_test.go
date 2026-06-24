package secretmgmt

import (
	"errors"
	"testing"
)

func TestNewSecretRef_Happy(t *testing.T) {
	r, err := NewSecretRef("github-pat")
	if err != nil {
		t.Fatal(err)
	}
	if r.Scheme() != "secret" {
		t.Fatalf("scheme: %s", r.Scheme())
	}
	if r.Name() != "github-pat" {
		t.Fatalf("name: %s", r.Name())
	}
	if r.String() != "secret:github-pat" {
		t.Fatalf("String(): %s", r.String())
	}
	if r.IsZero() {
		t.Fatal()
	}
}

func TestNewSecretRef_InvalidName(t *testing.T) {
	if _, err := NewSecretRef(""); err == nil {
		t.Fatal("empty name should reject")
	}
	if _, err := NewSecretRef("with spaces"); err == nil {
		t.Fatal("space in name should reject")
	}
}

func TestParseSecretRef_Happy(t *testing.T) {
	r, err := ParseSecretRef("secret:github-pat")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name() != "github-pat" {
		t.Fatalf("name: %s", r.Name())
	}
}

func TestParseSecretRef_HappyWithSpaces(t *testing.T) {
	r, err := ParseSecretRef("  secret:my-secret  ")
	if err != nil {
		t.Fatalf("expected trim ok, got %v", err)
	}
	if r.String() != "secret:my-secret" {
		t.Fatalf("String(): %s", r.String())
	}
}

func TestParseSecretRef_InvalidCases(t *testing.T) {
	cases := []string{
		"",
		":",
		"github-pat",     // no scheme
		"secret:",        // no name
		":github-pat",    // empty scheme
		"env:GITHUB_PAT", // unsupported scheme
		"file:/path",     // unsupported scheme
		"secret:with spaces",
		"secret:bad@char",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := ParseSecretRef(c)
			if !errors.Is(err, ErrInvalidSecretRef) {
				t.Fatalf("expected ErrInvalidSecretRef for %q, got %v", c, err)
			}
		})
	}
}

func TestIsSecretRefValue(t *testing.T) {
	if !IsSecretRefValue("secret:my-token") {
		t.Fatal()
	}
	if IsSecretRefValue("just-a-string") {
		t.Fatal()
	}
	if IsSecretRefValue("env:VAR") {
		t.Fatal("only secret scheme allowed in v2")
	}
}

func TestSecretRef_Zero(t *testing.T) {
	var r SecretRef
	if !r.IsZero() {
		t.Fatal("zero value should be IsZero=true")
	}
	if r.String() != "" {
		t.Fatalf("zero String(): %q", r.String())
	}
}
