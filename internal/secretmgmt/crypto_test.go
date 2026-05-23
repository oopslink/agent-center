package secretmgmt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMasterKey_NewAndEncryptDecryptRoundtrip(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("ghp_TestSecret123_PleaseDoNotLeak")
	ciphertext, nonce, err := mk.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) == string(plain) {
		t.Fatal("ciphertext should differ from plaintext")
	}
	decrypted, err := mk.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != string(plain) {
		t.Fatalf("decrypted: %s want %s", decrypted, plain)
	}
}

func TestMasterKey_Decrypt_TamperedCiphertext(t *testing.T) {
	mk, _ := GenerateMasterKey()
	plain := []byte("secret")
	ct, nonce, _ := mk.Encrypt(plain)
	// Flip a bit.
	ct[0] ^= 1
	if _, err := mk.Decrypt(ct, nonce); err == nil {
		t.Fatal("expected auth tag mismatch error")
	}
}

func TestMasterKey_Decrypt_WrongKey(t *testing.T) {
	mk1, _ := GenerateMasterKey()
	mk2, _ := GenerateMasterKey()
	ct, nonce, _ := mk1.Encrypt([]byte("hi"))
	if _, err := mk2.Decrypt(ct, nonce); err == nil {
		t.Fatal("wrong key should fail")
	}
}

func TestMasterKey_DifferentNonceEachEncrypt(t *testing.T) {
	mk, _ := GenerateMasterKey()
	_, nonce1, _ := mk.Encrypt([]byte("a"))
	_, nonce2, _ := mk.Encrypt([]byte("a"))
	if string(nonce1) == string(nonce2) {
		t.Fatal("nonces should differ")
	}
}

func TestNewMasterKey_InvalidSize(t *testing.T) {
	if _, err := NewMasterKey([]byte("too-short")); !errors.Is(err, ErrMasterKeyInvalidSize) {
		t.Fatalf("expected invalid size, got %v", err)
	}
}

func TestLoadMasterKey_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	mk, _ := GenerateMasterKey()
	if err := os.WriteFile(path, []byte(mk.Base64()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadMasterKey(path, false)
	if err != nil {
		t.Fatalf("LoadMasterKey: %v", err)
	}
	if loaded.Base64() != mk.Base64() {
		t.Fatal("roundtrip mismatch")
	}
}

func TestLoadMasterKey_FileMissing(t *testing.T) {
	_, err := LoadMasterKey(filepath.Join(t.TempDir(), "no-such-file"), true)
	if !errors.Is(err, ErrMasterKeyFileMissing) {
		t.Fatalf("expected file missing, got %v", err)
	}
}

func TestLoadMasterKey_BadPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	mk, _ := GenerateMasterKey()
	if err := os.WriteFile(path, []byte(mk.Base64()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMasterKey(path, false)
	if !errors.Is(err, ErrMasterKeyFileBadPerms) {
		t.Fatalf("expected bad perms, got %v", err)
	}
	// skip-perms-check should bypass.
	if _, err := LoadMasterKey(path, true); err != nil {
		t.Fatalf("skipPermsCheck should bypass: %v", err)
	}
}

func TestLoadMasterKey_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	if err := os.WriteFile(path, []byte("\n\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMasterKey(path, false); err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestMasterKey_NilEncryptDecrypt(t *testing.T) {
	var nilKey *MasterKey
	if _, _, err := nilKey.Encrypt([]byte("x")); !errors.Is(err, ErrMasterKeyNotLoaded) {
		t.Fatalf("nil encrypt should error, got %v", err)
	}
	if _, err := nilKey.Decrypt([]byte("c"), []byte("n")); !errors.Is(err, ErrMasterKeyNotLoaded) {
		t.Fatalf("nil decrypt should error, got %v", err)
	}
}
