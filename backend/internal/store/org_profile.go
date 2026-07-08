package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// OrgProfile is an org's avatar state. The organization name lives in WorkOS,
// not here. AvatarImage is nil until an image is uploaded; the client then
// renders a generated avatar from AvatarSeed.
type OrgProfile struct {
	OrgID             string
	AvatarSeed        string
	AvatarImage       []byte
	AvatarContentType string
}

// newAvatarSeed returns a fresh random seed for the generated avatar.
func newAvatarSeed() string {
	b := make([]byte, 8)
	rand.Read(b) // crypto/rand.Read never fails
	return hex.EncodeToString(b)
}

// GetOrgProfile returns the org's profile row, creating it with a random
// avatar seed on first read.
func (s *Store) GetOrgProfile(ctx context.Context, orgID string) (OrgProfile, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO org_profile (org_id, avatar_seed)
		VALUES ($1, $2)
		ON CONFLICT (org_id) DO UPDATE SET org_id = org_profile.org_id
		RETURNING org_id, avatar_seed, avatar_image, avatar_content_type
	`, orgID, newAvatarSeed())
	var p OrgProfile
	if err := row.Scan(&p.OrgID, &p.AvatarSeed, &p.AvatarImage, &p.AvatarContentType); err != nil {
		return OrgProfile{}, fmt.Errorf("store: get org profile: %w", err)
	}
	return p, nil
}

// SetOrgAvatarImage stores an uploaded avatar image for the org.
func (s *Store) SetOrgAvatarImage(ctx context.Context, orgID string, image []byte, contentType string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO org_profile (org_id, avatar_seed, avatar_image, avatar_content_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id) DO UPDATE
		SET avatar_image = $3, avatar_content_type = $4, updated_at = now()
	`, orgID, newAvatarSeed(), image, contentType)
	if err != nil {
		return fmt.Errorf("store: set org avatar image: %w", err)
	}
	return nil
}

// ClearOrgAvatarImage removes the uploaded image so the org falls back to its
// generated avatar.
func (s *Store) ClearOrgAvatarImage(ctx context.Context, orgID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE org_profile
		SET avatar_image = NULL, avatar_content_type = '', updated_at = now()
		WHERE org_id = $1
	`, orgID)
	if err != nil {
		return fmt.Errorf("store: clear org avatar image: %w", err)
	}
	return nil
}

// RegenerateOrgAvatarSeed re-rolls the org's generated-avatar seed and clears
// any uploaded image, so the fresh generated avatar actually shows.
func (s *Store) RegenerateOrgAvatarSeed(ctx context.Context, orgID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO org_profile (org_id, avatar_seed)
		VALUES ($1, $2)
		ON CONFLICT (org_id) DO UPDATE
		SET avatar_seed = $2, avatar_image = NULL, avatar_content_type = '', updated_at = now()
	`, orgID, newAvatarSeed())
	if err != nil {
		return fmt.Errorf("store: regenerate org avatar seed: %w", err)
	}
	return nil
}
