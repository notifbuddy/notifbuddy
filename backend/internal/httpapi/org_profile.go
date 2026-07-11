package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
)

// avatarMaxBytes caps the decoded size of an uploaded organization avatar.
// Clients downscale before uploading, so this is a backstop, not a target.
const avatarMaxBytes = 512 * 1024

// orgAdminOnlyMsg is the 403 body for organization-profile mutations.
const orgAdminOnlyMsg = "only admins can edit the organization profile"

// avatarContentTypes are the image types an uploaded avatar may use.
var avatarContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// GetOrganizationProfile implements `getOrganizationProfile`: GET /organization/profile.
// Returns the org's name (from WorkOS) and avatar state (from the store). The
// profile row — including its random avatar seed — is created lazily here.
func (h Handler) GetOrganizationProfile(ctx context.Context) (api.GetOrganizationProfileRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.GetOrganizationProfileUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" || h.store == nil {
		return &api.GetOrganizationProfileBadRequest{Message: "no active organization"}, nil
	}
	resp, err := h.orgProfileResponse(ctx, user.OrgID)
	if err != nil {
		return &api.GetOrganizationProfileBadRequest{Message: "failed to load the organization profile"}, nil
	}
	return resp, nil
}

// UpdateOrganizationProfile implements `updateOrganizationProfile`: PUT /organization/profile.
// Renames the organization in WorkOS. Admin-only.
func (h Handler) UpdateOrganizationProfile(ctx context.Context, req *api.UpdateOrgProfileRequest) (api.UpdateOrganizationProfileRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.UpdateOrganizationProfileUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" || h.store == nil {
		return &api.UpdateOrganizationProfileBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.UpdateOrganizationProfileForbidden{Message: orgAdminOnlyMsg}, nil
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return &api.UpdateOrganizationProfileBadRequest{Message: "name must not be empty"}, nil
	}
	if _, err := h.auth.UpdateOrganizationName(ctx, user.OrgID, name); err != nil {
		msg := "failed to rename the organization"
		var userMsg auth.UserMessageError
		if errors.As(err, &userMsg) {
			msg = userMsg.Msg
		}
		return &api.UpdateOrganizationProfileBadRequest{Message: msg}, nil
	}
	resp, err := h.orgProfileResponse(ctx, user.OrgID)
	if err != nil {
		return &api.UpdateOrganizationProfileBadRequest{Message: "failed to load the organization profile"}, nil
	}
	return resp, nil
}

// UploadOrganizationAvatar implements `uploadOrganizationAvatar`: PUT /organization/avatar.
// Stores an uploaded avatar image sent as a data URL. Admin-only.
func (h Handler) UploadOrganizationAvatar(ctx context.Context, req *api.UploadOrgAvatarRequest) (api.UploadOrganizationAvatarRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.UploadOrganizationAvatarUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" || h.store == nil {
		return &api.UploadOrganizationAvatarBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.UploadOrganizationAvatarForbidden{Message: orgAdminOnlyMsg}, nil
	}
	contentType, image, err := decodeImageDataURL(req.ImageDataUrl)
	if err != nil {
		return &api.UploadOrganizationAvatarBadRequest{Message: err.Error()}, nil
	}
	if err := h.store.SetOrgAvatarImage(ctx, user.OrgID, image, contentType); err != nil {
		slog.ErrorContext(ctx, "httpapi: set org avatar failed", "org_id", user.OrgID, "error", err)
		return &api.UploadOrganizationAvatarBadRequest{Message: "failed to store the avatar"}, nil
	}
	resp, err := h.orgProfileResponse(ctx, user.OrgID)
	if err != nil {
		return &api.UploadOrganizationAvatarBadRequest{Message: "failed to load the organization profile"}, nil
	}
	return resp, nil
}

// DeleteOrganizationAvatar implements `deleteOrganizationAvatar`: DELETE /organization/avatar.
// Removes the uploaded image; the org falls back to its generated avatar. Admin-only.
func (h Handler) DeleteOrganizationAvatar(ctx context.Context) (api.DeleteOrganizationAvatarRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.DeleteOrganizationAvatarUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" || h.store == nil {
		return &api.DeleteOrganizationAvatarBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.DeleteOrganizationAvatarForbidden{Message: orgAdminOnlyMsg}, nil
	}
	if err := h.store.ClearOrgAvatarImage(ctx, user.OrgID); err != nil {
		slog.ErrorContext(ctx, "httpapi: clear org avatar failed", "org_id", user.OrgID, "error", err)
		return &api.DeleteOrganizationAvatarBadRequest{Message: "failed to remove the avatar"}, nil
	}
	resp, err := h.orgProfileResponse(ctx, user.OrgID)
	if err != nil {
		return &api.DeleteOrganizationAvatarBadRequest{Message: "failed to load the organization profile"}, nil
	}
	return resp, nil
}

// RegenerateOrganizationAvatar implements `regenerateOrganizationAvatar`:
// POST /organization/avatar/regenerate. Re-rolls the generated avatar's seed
// and clears any uploaded image. Admin-only.
func (h Handler) RegenerateOrganizationAvatar(ctx context.Context) (api.RegenerateOrganizationAvatarRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.RegenerateOrganizationAvatarUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" || h.store == nil {
		return &api.RegenerateOrganizationAvatarBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.RegenerateOrganizationAvatarForbidden{Message: orgAdminOnlyMsg}, nil
	}
	if err := h.store.RegenerateOrgAvatarSeed(ctx, user.OrgID); err != nil {
		slog.ErrorContext(ctx, "httpapi: regenerate org avatar failed", "org_id", user.OrgID, "error", err)
		return &api.RegenerateOrganizationAvatarBadRequest{Message: "failed to regenerate the avatar"}, nil
	}
	resp, err := h.orgProfileResponse(ctx, user.OrgID)
	if err != nil {
		return &api.RegenerateOrganizationAvatarBadRequest{Message: "failed to load the organization profile"}, nil
	}
	return resp, nil
}

// orgProfileResponse assembles the shared response: the org name from WorkOS
// plus the avatar state from the store (uploaded image as a data URL).
func (h Handler) orgProfileResponse(ctx context.Context, orgID string) (*api.OrgProfileResponse, error) {
	name, err := h.auth.GetOrganizationName(ctx, orgID)
	if err != nil {
		return nil, err
	}
	p, err := h.store.GetOrgProfile(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "httpapi: get org profile failed", "org_id", orgID, "error", err)
		return nil, err
	}
	resp := &api.OrgProfileResponse{ID: orgID, Name: name, AvatarSeed: p.AvatarSeed}
	if len(p.AvatarImage) > 0 && p.AvatarContentType != "" {
		resp.AvatarUrl = api.NewOptString(
			"data:" + p.AvatarContentType + ";base64," + base64.StdEncoding.EncodeToString(p.AvatarImage))
	}
	return resp, nil
}

// decodeImageDataURL parses a `data:image/...;base64,...` URL into its content
// type and bytes, enforcing the allowed types and the size cap.
func decodeImageDataURL(dataURL string) (contentType string, image []byte, err error) {
	rest, ok := strings.CutPrefix(dataURL, "data:")
	if !ok {
		return "", nil, fmt.Errorf("imageDataUrl must be a data: URL")
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return "", nil, fmt.Errorf("imageDataUrl is malformed")
	}
	contentType, ok = strings.CutSuffix(meta, ";base64")
	if !ok {
		return "", nil, fmt.Errorf("imageDataUrl must be base64-encoded")
	}
	if !avatarContentTypes[contentType] {
		return "", nil, fmt.Errorf("unsupported image type %q — use PNG, JPEG, or WebP", contentType)
	}
	image, err = base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, fmt.Errorf("imageDataUrl payload is not valid base64")
	}
	if len(image) == 0 {
		return "", nil, fmt.Errorf("image is empty")
	}
	if len(image) > avatarMaxBytes {
		return "", nil, fmt.Errorf("image is too large — at most %d KiB", avatarMaxBytes/1024)
	}
	return contentType, image, nil
}
