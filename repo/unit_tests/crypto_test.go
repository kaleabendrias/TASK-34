package unit_tests

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/harborworks/booking-hub/internal/infrastructure/crypto"
)

func TestKeyManagerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "master.key")
	km, err := crypto.LoadOrCreate(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if km.KeyPath() != keyPath {
		t.Errorf("KeyPath mismatch")
	}

	plain := []byte("this is a sensitive booking note")
	ct, err := km.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatal("ciphertext should differ from plaintext")
	}
	got, err := km.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestKeyManagerEmptyInputs(t *testing.T) {
	km, _ := crypto.LoadOrCreate(filepath.Join(t.TempDir(), "k.key"))
	if got, err := km.Encrypt(nil); err != nil || got != nil {
		t.Fatalf("encrypt(nil) → got=%v err=%v", got, err)
	}
	if got, err := km.Decrypt(nil); err != nil || got != nil {
		t.Fatalf("decrypt(nil) → got=%v err=%v", got, err)
	}
}

func TestKeyManagerDecryptTooShort(t *testing.T) {
	km, _ := crypto.LoadOrCreate(filepath.Join(t.TempDir(), "k.key"))
	if _, err := km.Decrypt([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error on too-short ciphertext")
	}
}

func TestKeyManagerPersistence(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "master.key")
	km1, err := crypto.LoadOrCreate(keyPath)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	km2, err := crypto.LoadOrCreate(keyPath)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	plain := []byte("hello")
	ct, _ := km1.Encrypt(plain)
	got, err := km2.Decrypt(ct)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("second loader should decrypt first loader's ciphertext (got=%v err=%v)", got, err)
	}
}

func TestKeyManagerWrongKeyFails(t *testing.T) {
	km1, _ := crypto.LoadOrCreate(filepath.Join(t.TempDir(), "k.key"))
	km2, _ := crypto.LoadOrCreate(filepath.Join(t.TempDir(), "k.key"))
	ct, _ := km1.Encrypt([]byte("hello"))
	if _, err := km2.Decrypt(ct); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestKeyManagerRejectsBadFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(bad, []byte("too short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.LoadOrCreate(bad); err == nil {
		t.Fatal("expected error on wrong-size key file")
	}
}
