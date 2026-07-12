package crypto

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// fakeKMS is a KMSClient that XORs with a fixed byte — enough to prove the
// kmsEncryptor adapter passes the right key ID and round-trips payloads.
type fakeKMS struct {
	gotKeyID string
	err      error
}

func (f *fakeKMS) Encrypt(_ context.Context, keyID string, plaintext []byte) ([]byte, error) {
	f.gotKeyID = keyID
	if f.err != nil {
		return nil, f.err
	}
	return xor(plaintext), nil
}

func (f *fakeKMS) Decrypt(_ context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	f.gotKeyID = keyID
	if f.err != nil {
		return nil, f.err
	}
	return xor(ciphertext), nil
}

func xor(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = c ^ 0x5a
	}
	return out
}

func TestKMSEncryptorRoundTrip(t *testing.T) {
	fake := &fakeKMS{}
	enc, err := NewKMSEncryptor(context.Background(), fake, "projects/p/locations/l/keyRings/r/cryptoKeys/k")
	if err != nil {
		t.Fatalf("NewKMSEncryptor: %v", err)
	}

	plaintext := []byte("xoxb-slack-token")
	ct, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}
	if fake.gotKeyID != "projects/p/locations/l/keyRings/r/cryptoKeys/k" {
		t.Fatalf("key ID not forwarded, got %q", fake.gotKeyID)
	}

	pt, err := enc.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("round trip mismatch: got %q", pt)
	}
}

func TestKMSEncryptorPropagatesErrors(t *testing.T) {
	fake := &fakeKMS{err: errors.New("kms unavailable")}
	enc, err := NewKMSEncryptor(context.Background(), fake, "key")
	if err != nil {
		t.Fatalf("NewKMSEncryptor: %v", err)
	}
	if _, err := enc.Encrypt([]byte("x")); err == nil {
		t.Fatal("Encrypt: expected error")
	}
	if _, err := enc.Decrypt([]byte("x")); err == nil {
		t.Fatal("Decrypt: expected error")
	}
}

func TestNewKMSEncryptorValidation(t *testing.T) {
	if _, err := NewKMSEncryptor(context.Background(), nil, "key"); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := NewKMSEncryptor(context.Background(), &fakeKMS{}, ""); err == nil {
		t.Fatal("empty key ID accepted")
	}
}

// Known-answer check for the Castagnoli checksum Cloud KMS integrity
// verification depends on ("123456789" is the standard CRC-32C test vector).
func TestCRC32C(t *testing.T) {
	if got := crc32c([]byte("123456789")); got != 0xe3069283 {
		t.Fatalf("crc32c: got %#x, want 0xe3069283", got)
	}
}
