package integrations

import (
	"context"
	"fmt"

	"xolo/backend/internal/store"
)

// SlackMemberView is a synced Slack member (bot or human) for the settings UI's
// auto-add pickers.
type SlackMemberView struct {
	MemberID string `json:"memberId"` // Slack U… id (stored on the config, used to invite)
	Name     string `json:"name"`     // display / real name, falling back to handle
	IconURL  string `json:"iconUrl"`
	IsBot    bool   `json:"isBot"`
}

// SyncSlackMembers fetches the workspace's members via the Slack API and replaces
// the org's stored snapshot. Called on connect and by the manual Sync action.
func (s *Service) SyncSlackMembers(ctx context.Context, orgID string) error {
	if !s.Enabled() {
		return fmt.Errorf("integrations: not configured")
	}
	token, err := s.SlackBotToken(ctx, orgID)
	if err != nil {
		return err
	}
	users, err := s.slack.ListUsers(ctx, token)
	if err != nil {
		return err
	}
	rows := make([]store.SlackMember, 0, len(users))
	for _, u := range users {
		if u.ID == "" {
			continue
		}
		rows = append(rows, store.SlackMember{
			MemberID: u.ID,
			Name:     u.Name, // toUser() already prefers real_name over handle
			RealName: u.Name,
			IconURL:  u.IconURL,
			IsBot:    u.IsBot,
		})
	}
	return s.store.ReplaceSlackMembers(ctx, orgID, rows)
}

// GetSlackMembers returns the org's synced Slack members for the settings UI
// (empty slice when nothing synced yet).
func (s *Service) GetSlackMembers(ctx context.Context, orgID string) ([]SlackMemberView, error) {
	if !s.Enabled() {
		return []SlackMemberView{}, nil
	}
	rows, err := s.store.GetSlackMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]SlackMemberView, 0, len(rows))
	for _, m := range rows {
		name := m.RealName
		if name == "" {
			name = m.Name
		}
		out = append(out, SlackMemberView{
			MemberID: m.MemberID,
			Name:     name,
			IconURL:  m.IconURL,
			IsBot:    m.IsBot,
		})
	}
	return out, nil
}
