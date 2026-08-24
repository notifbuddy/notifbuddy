package cmd

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"xolo/cli/internal/browser"
	"xolo/cli/internal/tokenstore"
)

func newLoginCmd(a *app) *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in via your browser (device flow)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			ac := a.authd()

			dc, err := ac.StartDeviceFlow(ctx)
			if err != nil {
				return err
			}
			verifyURL := a.dashboardURL() + "/device?user_code=" + url.QueryEscape(dc.UserCode)

			errOut := cmd.ErrOrStderr()
			fmt.Fprintf(errOut, "First, copy your one-time code: %s\n", dc.UserCode)
			fmt.Fprintf(errOut, "Then approve the sign-in at: %s\n", verifyURL)
			if !noBrowser {
				if err := browser.Open(verifyURL); err == nil {
					fmt.Fprintln(errOut, "Opened your browser — approve the request there.")
				}
			}
			fmt.Fprintln(errOut, "Waiting for approval…")

			token, err := ac.PollForToken(ctx, dc)
			if err != nil {
				return err
			}
			if err := tokenstore.Save(a.authURL(), token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			sess, err := ac.GetSession(ctx, token)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, map[string]any{
					"status":         "logged_in",
					"email":          sess.User.Email,
					"userId":         sess.User.ID,
					"organizationId": sess.Session.ActiveOrganizationID,
				})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Signed in as %s\n", sess.User.Email)
			if sess.Session.ActiveOrganizationID == "" {
				orgs, _ := ac.ListOrganizations(ctx, token)
				if len(orgs) == 0 {
					fmt.Fprintln(out, "No organization yet — create one with: notifbuddy org create <name>")
				} else {
					fmt.Fprintln(out, "Pick an organization with: notifbuddy org switch <name>")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the approval URL instead of opening a browser")
	return cmd
}

func newLogoutCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out and forget the stored token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if token, err := a.token(); err == nil {
				_ = a.authd().SignOut(cmd.Context(), token)
			}
			if err := tokenstore.Clear(a.authURL()); err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, map[string]any{"status": "logged_out"})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Signed out.")
			return nil
		},
	}
}
