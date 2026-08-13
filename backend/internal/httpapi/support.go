package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
)

const defaultChatwootBaseURL = "https://app.chatwoot.com"

func (h Handler) GetSupportChat(ctx context.Context) (*api.SupportChatResponse, error) {
	if h.chatwoot.WebsiteToken == "" {
		return &api.SupportChatResponse{Enabled: false}, nil
	}
	baseURL := strings.TrimSuffix(h.chatwoot.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultChatwootBaseURL
	}
	resp := &api.SupportChatResponse{
		Enabled:      true,
		BaseUrl:      api.NewOptString(baseURL),
		WebsiteToken: api.NewOptString(h.chatwoot.WebsiteToken),
	}
	user := auth.UserFromContext(ctx)
	if user == nil {
		return resp, nil
	}

	resp.Identifier = api.NewOptString(user.ID)
	resp.Email = api.NewOptString(user.Email)
	if name := strings.TrimSpace(user.FirstName + " " + user.LastName); name != "" {
		resp.Name = api.NewOptString(name)
	}
	if h.chatwoot.HMACToken != "" {
		resp.IdentifierHash = api.NewOptString(hmacHex(h.chatwoot.HMACToken, user.ID))
	}
	if user.OrgID != "" {
		resp.OrganizationId = api.NewOptString(user.OrgID)
		for _, m := range h.auth.ListUserOrganizations(ctx, user.ID, orgListLimit) {
			if m.ID == user.OrgID {
				resp.OrganizationName = api.NewOptString(m.Name)
				break
			}
		}
		if status, err := h.billing.StatusForOrg(ctx, user.OrgID); err == nil {
			resp.Plan = api.NewOptString(string(status.Plan))
		}
	}
	return resp, nil
}

func hmacHex(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
