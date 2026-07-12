package crypto

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// GCPKMSClient implements KMSClient over Google Cloud KMS. The keyID passed
// to Encrypt/Decrypt is the full crypto-key resource name
// (projects/*/locations/*/keyRings/*/cryptoKeys/*, created by infra).
// Decrypt works against any active version of the key, so rotation needs no
// application changes — old rows keep decrypting via their original version.
type GCPKMSClient struct {
	client *kms.KeyManagementClient
}

// NewGCPKMSClient dials Cloud KMS using Application Default Credentials (the
// Cloud Run runtime service account in prod, `gcloud auth application-default
// login` locally). The client is process-lived; Close it on shutdown.
func NewGCPKMSClient(ctx context.Context) (*GCPKMSClient, error) {
	c, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcp kms client: %w", err)
	}
	return &GCPKMSClient{client: c}, nil
}

func (g *GCPKMSClient) Close() error { return g.client.Close() }

// crc32cTable is the Castagnoli polynomial Cloud KMS uses for its
// request/response integrity checksums.
var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

func crc32c(b []byte) int64 { return int64(crc32.Checksum(b, crc32cTable)) }

// Encrypt seals plaintext with the key's primary version, verifying the
// end-to-end integrity fields Cloud KMS provides (request checksum echoed as
// verified, response ciphertext checksum matching).
func (g *GCPKMSClient) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	resp, err := g.client.Encrypt(ctx, &kmspb.EncryptRequest{
		Name:            keyID,
		Plaintext:       plaintext,
		PlaintextCrc32C: wrapperspb.Int64(crc32c(plaintext)),
	})
	if err != nil {
		return nil, fmt.Errorf("crypto: gcp kms encrypt: %w", err)
	}
	if !resp.VerifiedPlaintextCrc32C {
		return nil, errors.New("crypto: gcp kms encrypt: request corrupted in transit (plaintext checksum unverified)")
	}
	if resp.CiphertextCrc32C == nil || crc32c(resp.Ciphertext) != resp.CiphertextCrc32C.Value {
		return nil, errors.New("crypto: gcp kms encrypt: response corrupted in transit (ciphertext checksum mismatch)")
	}
	return resp.Ciphertext, nil
}

// Decrypt reverses Encrypt; Cloud KMS selects the key version from the
// ciphertext itself, so keyID stays the crypto-key name across rotations.
func (g *GCPKMSClient) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	resp, err := g.client.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:             keyID,
		Ciphertext:       ciphertext,
		CiphertextCrc32C: wrapperspb.Int64(crc32c(ciphertext)),
	})
	if err != nil {
		return nil, fmt.Errorf("crypto: gcp kms decrypt: %w", err)
	}
	if resp.PlaintextCrc32C == nil || crc32c(resp.Plaintext) != resp.PlaintextCrc32C.Value {
		return nil, errors.New("crypto: gcp kms decrypt: response corrupted in transit (plaintext checksum mismatch)")
	}
	return resp.Plaintext, nil
}
