package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"xolo/cli/internal/api"
	"xolo/cli/internal/authd"
)

func newOrgCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage organizations",
	}
	cmd.AddCommand(newOrgListCmd(a), newOrgCreateCmd(a), newOrgSwitchCmd(a))
	return cmd
}

func newOrgListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your organizations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			token, err := a.token()
			if err != nil {
				return err
			}
			ac := a.authd()
			orgs, err := ac.ListOrganizations(ctx, token)
			if err != nil {
				return err
			}
			activeID := ""
			if sess, err := ac.GetSession(ctx, token); err == nil {
				activeID = sess.Session.ActiveOrganizationID
			}
			if a.jsonOut {
				type row struct {
					authd.Org
					Active bool `json:"active"`
				}
				rows := make([]row, 0, len(orgs))
				for _, o := range orgs {
					rows = append(rows, row{Org: o, Active: o.ID == activeID})
				}
				return a.printJSON(cmd, rows)
			}
			out := cmd.OutOrStdout()
			if len(orgs) == 0 {
				fmt.Fprintln(out, "No organizations. Create one with: notifbuddy org create <name>")
				return nil
			}
			for _, o := range orgs {
				marker := "  "
				if o.ID == activeID {
					marker = "* "
				}
				fmt.Fprintf(out, "%s%s\t%s\n", marker, o.Name, o.ID)
			}
			return nil
		},
	}
}

func newOrgCreateCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create an organization and make it active",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			res, err := c.CreateOrganization(cmd.Context(), &api.CreateOrganizationRequest{Name: args[0]})
			me, err := apiResult[api.UserResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, me)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created organization %q — it is now active.\n", args[0])
			return nil
		},
	}
}

func newOrgSwitchCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <name|slug|id>",
		Short: "Set the active organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			token, err := a.token()
			if err != nil {
				return err
			}
			ac := a.authd()
			orgs, err := ac.ListOrganizations(ctx, token)
			if err != nil {
				return err
			}
			want := strings.ToLower(args[0])
			var match *authd.Org
			for i, o := range orgs {
				if o.ID == args[0] || o.Slug == args[0] || strings.ToLower(o.Name) == want {
					match = &orgs[i]
					break
				}
			}
			if match == nil {
				return fmt.Errorf("no organization matches %q — run `notifbuddy org list`", args[0])
			}
			if err := ac.SetActiveOrganization(ctx, token, match.ID); err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, match)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active organization: %s\n", match.Name)
			return nil
		},
	}
}
