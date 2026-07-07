package billing

import (
	"testing"
	"time"

	"xolo/backend/internal/store"
)

func TestDerive(t *testing.T) {
	at := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	future := at.Add(24 * time.Hour)
	past := at.Add(-24 * time.Hour)

	tests := []struct {
		name           string
		row            store.OrgBilling
		wantLocked     bool
		wantSubscribed bool
	}{
		{
			name:       "trial active",
			row:        store.OrgBilling{Plan: PlanTrial, TrialEndsAt: future},
			wantLocked: false,
		},
		{
			name:       "trial expired",
			row:        store.OrgBilling{Plan: PlanTrial, TrialEndsAt: past},
			wantLocked: true,
		},
		{
			name:       "trial expires exactly now",
			row:        store.OrgBilling{Plan: PlanTrial, TrialEndsAt: at},
			wantLocked: true,
		},
		{
			name: "pro active",
			row: store.OrgBilling{Plan: PlanPro, StripeStatus: "active",
				StripeSubscriptionID: "sub_1", TrialEndsAt: past},
			wantLocked:     false,
			wantSubscribed: true,
		},
		{
			name: "pro past_due stays unlocked during retries",
			row: store.OrgBilling{Plan: PlanPro, StripeStatus: "past_due",
				StripeSubscriptionID: "sub_1", TrialEndsAt: past},
			wantLocked:     false,
			wantSubscribed: true,
		},
		{
			name: "pro canceled after trial locks",
			row: store.OrgBilling{Plan: PlanPro, StripeStatus: "canceled",
				TrialEndsAt: past},
			wantLocked: true,
		},
		{
			name: "pro canceled during trial stays unlocked",
			row: store.OrgBilling{Plan: PlanPro, StripeStatus: "canceled",
				TrialEndsAt: future},
			wantLocked: false,
		},
		{
			name: "pro unpaid after trial locks",
			row: store.OrgBilling{Plan: PlanPro, StripeStatus: "unpaid",
				StripeSubscriptionID: "sub_1", TrialEndsAt: past},
			wantLocked:     true,
			wantSubscribed: true,
		},
		{
			name:       "oss_free never locks",
			row:        store.OrgBilling{Plan: PlanOSSFree, TrialEndsAt: past},
			wantLocked: false,
		},
		{
			name:       "enterprise never locks",
			row:        store.OrgBilling{Plan: PlanEnterprise, TrialEndsAt: past},
			wantLocked: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derive(&tt.row, at)
			if got.Locked != tt.wantLocked {
				t.Errorf("Locked = %v, want %v", got.Locked, tt.wantLocked)
			}
			if got.Subscribed != tt.wantSubscribed {
				t.Errorf("Subscribed = %v, want %v", got.Subscribed, tt.wantSubscribed)
			}
		})
	}
}
