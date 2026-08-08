package auth

import "testing"

func TestNormalizeInvitationState(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"canceled", InvitationCanceled},
		{"CANCELED", InvitationCanceled},
		{" canceled ", InvitationCanceled},
		{"pending", InvitationPending},
		{"accepted", InvitationAccepted},
		{"rejected", InvitationRejected},
		{"", InvitationPending},
		{"cancelled", InvitationPending},
		{"something-new", InvitationPending},
	}
	for _, c := range cases {
		if got := normalizeInvitationState(c.status); got != c.want {
			t.Errorf("normalizeInvitationState(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

func TestToInvitationNormalizesStateAndOwnerRole(t *testing.T) {
	entry := invitationEntry{
		ID:             "invitation_1",
		Email:          "teammate@example.com",
		Status:         "canceled",
		ExpiresAt:      "2026-07-03T00:00:00Z",
		Role:           roleOwner,
		OrganizationID: "org_1",
	}
	got := entry.toInvitation()
	if got.State != InvitationCanceled {
		t.Errorf("State = %q, want %q", got.State, InvitationCanceled)
	}
	if got.Role != RoleAdmin {
		t.Errorf("Role = %q, want %q", got.Role, RoleAdmin)
	}
	if got.Email != entry.Email || got.ID != entry.ID || got.ExpiresAt != entry.ExpiresAt {
		t.Errorf("passthrough fields altered: %+v", got)
	}
}
