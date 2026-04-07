package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeyManager wraps a 32-byte AES-256 key kept in a locally managed file. The
// key is generated on first start if the file is missing, and is stored on a
// docker volume mounted at /app/keys (mode 0600). All sensitive content (e.g.
// booking notes) is encrypted with AES-GCM under this key.
type KeyManager struct {
	keyPath string
	key     []byte
	gcm     cipher.AEAD
}

// LoadOrCreate loads an existing master key or generates a new one.
func LoadOrCreate(keyPath string) (*KeyManager, error) {
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("ensure key dir: %w", err)
	}

	var keyBytes []byte
	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("key file %s is %d bytes, expected 32", keyPath, len(data))
		}
		keyBytes = data
	} else if errors.Is(err, os.ErrNotExist) {
		keyBytes = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, keyBytes); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := os.WriteFile(keyPath, keyBytes, 0o600); err != nil {
			return nil, fmt.Errorf("write key: %w", err)
		}
	} else {
		return nil, fmt.Errorf("read key: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &KeyManager{keyPath: keyPath, key: keyBytes, gcm: gcm}, nil
}

// Encrypt produces nonce||ciphertext for the given plaintext. Empty input
// returns nil so callers can transparently store nullable columns.
func (k *KeyManager) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	nonce := make([]byte, k.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return k.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt.
func (k *KeyManager) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	ns := k.gcm.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	return k.gcm.Open(nil, nonce, ct, nil)
}

// KeyPath returns the on-disk location for diagnostics.
func (k *KeyManager) KeyPath() string { return k.keyPath }
