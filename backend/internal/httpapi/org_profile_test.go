package httpapi

import (
	"context"
	"testing"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
	"xolo/backend/internal/featureflags"
	"xolo/backend/internal/store"
)

func TestUpdateOrganizationProfile_RejectsDevSettingsWhenFeatureOff(t *testing.T) {
	h := Handler{
		store: &store.Store{},
		flags: featureflags.Flags{DeveloperSettings: false},
	}
	ctx := auth.ContextWithUser(context.Background(), &auth.SessionUser{
		OrgID: "org1",
		Role:  auth.RoleAdmin,
	})
	res, err := h.UpdateOrganizationProfile(ctx, &api.UpdateOrgProfileRequest{
		DeveloperSettings: api.NewOptDeveloperSettings(api.DeveloperSettings{
			Enabled:     true,
			SyncEnabled: false,
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	forbidden, ok := res.(*api.UpdateOrganizationProfileForbidden)
	if !ok {
		t.Fatalf("want Forbidden, got %T", res)
	}
	if forbidden.Message != developerSettingsOffMsg {
		t.Errorf("message = %q, want %q", forbidden.Message, developerSettingsOffMsg)
	}
}

func TestUpdateOrganizationProfile_RejectsEmptyBody(t *testing.T) {
	h := Handler{
		store: &store.Store{},
		flags: featureflags.Flags{DeveloperSettings: true},
	}
	ctx := auth.ContextWithUser(context.Background(), &auth.SessionUser{
		OrgID: "org1",
		Role:  auth.RoleAdmin,
	})
	res, err := h.UpdateOrganizationProfile(ctx, &api.UpdateOrgProfileRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := res.(*api.UpdateOrganizationProfileBadRequest); !ok {
		t.Fatalf("want BadRequest, got %T", res)
	}
}
