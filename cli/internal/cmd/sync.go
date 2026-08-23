package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"xolo/cli/internal/api"
)

func newSyncCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Re-sync Linear teams, workflow states, and Slack members",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			res, err := c.SyncSettings(cmd.Context())
			resp, err := apiResult[api.LinearSettingsResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced: %d Linear teams, %d Slack members.\n", len(resp.Teams), len(resp.SlackMembers))
			return nil
		},
	}
}

func newWebhooksCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "webhooks",
		Short: "List recent Linear webhook deliveries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			res, err := c.ListLinearWebhooks(cmd.Context())
			resp, err := apiResult[api.WebhookListResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, resp)
			}
			if len(resp.Events) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No webhook deliveries recorded yet.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "RECEIVED\tTYPE\tACTION\tDELIVERY")
			for _, e := range resp.Events {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.ReceivedAt, e.EventType, e.Action.Value, e.DeliveryId)
			}
			return w.Flush()
		},
	}
}
