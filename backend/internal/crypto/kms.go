package crypto

import (
	"context"
	"errors"
	"fmt"
)

// KMSClient is the minimal surface a cloud KMS must provide for envelope-free
// direct encryption of small secrets. Implement this against your provider
// (AWS KMS Encrypt/Decrypt, GCP KMS, Vault transit, etc.) and pass it to
// NewKMSEncryptor. Both methods receive the configured key ID/ARN.
type KMSClient interface {
	Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error)
}

// kmsEncryptor adapts a KMSClient + key ID to the Encryptor interface. The
// context is fixed at construction time for the Encryptor interface's
// context-free methods; callers that need per-request contexts should hold the
// KMSClient directly.
type kmsEncryptor struct {
	client KMSClient
	keyID  string
	ctx    context.Context
}

// NewKMSEncryptor wraps a KMSClient so integration tokens are encrypted with a
// customer-managed KMS key. Wire your provider's client here.
func NewKMSEncryptor(ctx context.Context, client KMSClient, keyID string) (Encryptor, error) {
	if client == nil {
		return nil, errors.New("crypto: KMS client is nil")
	}
	if keyID == "" {
		return nil, errors.New("crypto: KMS key ID is required")
	}
	return &kmsEncryptor{client: client, keyID: keyID, ctx: ctx}, nil
}

func (e *kmsEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	ct, err := e.client.Encrypt(e.ctx, e.keyID, plaintext)
	if err != nil {
		return nil, fmt.Errorf("crypto: kms encrypt: %w", err)
	}
	return ct, nil
}

func (e *kmsEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	pt, err := e.client.Decrypt(e.ctx, e.keyID, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("crypto: kms decrypt: %w", err)
	}
	return pt, nil
}
