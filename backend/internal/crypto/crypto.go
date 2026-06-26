// Package crypto provides at-rest encryption for integration secrets (GitHub
// installation/Slack tokens) before they are written to the database.
//
// Encryption is pluggable behind the Encryptor interface so the same call sites
// work with a cloud KMS in production and a local symmetric key in development.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Encryptor encrypts and decrypts small secrets. Implementations must be safe
// for concurrent use. Ciphertext is opaque and self-describing enough for the
// matching Decrypt to reverse it (e.g. nonce-prefixed for AES-GCM).
type Encryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// localKeyEncryptor is an AES-256-GCM Encryptor backed by an in-process key.
// Suitable for local development; production should use a KMS-backed Encryptor.
type localKeyEncryptor struct {
	aead cipher.AEAD
}

// NewLocalKeyEncryptor builds an AES-256-GCM encryptor from a 32-byte key.
func NewLocalKeyEncryptor(key []byte) (Encryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: local key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &localKeyEncryptor{aead: aead}, nil
}

// NewLocalKeyEncryptorFromBase64 builds a local encryptor from a base64-encoded
// 32-byte key. An empty string generates a fresh random key (dev convenience),
// returning it so the caller can log/persist it; tokens encrypted with a
// generated key won't decrypt after a restart, which is acceptable for dev.
func NewLocalKeyEncryptorFromBase64(b64 string) (Encryptor, string, error) {
	if b64 == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, "", fmt.Errorf("crypto: generate key: %w", err)
		}
		enc, err := NewLocalKeyEncryptor(key)
		if err != nil {
			return nil, "", err
		}
		return enc, base64.StdEncoding.EncodeToString(key), nil
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", fmt.Errorf("crypto: decode key: %w", err)
	}
	enc, err := NewLocalKeyEncryptor(key)
	return enc, b64, err
}

// Encrypt seals plaintext with a fresh random nonce, returning nonce||ciphertext.
func (e *localKeyEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: nonce: %w", err)
	}
	// Seal appends the ciphertext to the nonce slice, so the result is
	// nonce||ciphertext — Decrypt splits it back apart.
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt, expecting nonce||ciphertext.
func (e *localKeyEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	ns := e.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	plaintext, err := e.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}
