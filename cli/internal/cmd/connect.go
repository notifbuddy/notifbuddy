package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"xolo/cli/internal/browser"
)

func newConnectCmd(a *app) *cobra.Command {
	var userLevel bool
	var noWait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:       "connect <slack|linear>",
		Short:     "Connect an integration (opens your browser)",
		Long:      "Starts the provider OAuth flow in your browser. Workspace level installs the org-wide connection; --user connects your personal account so actions are attributed to you.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"slack", "linear"},
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			if provider != "slack" && provider != "linear" {
				return fmt.Errorf("unknown provider %q (want slack or linear)", provider)
			}
			level := "workspace"
			if userLevel {
				level = "user"
			}

			if st, err := a.integrationStatus(cmd); err == nil {
				for _, in := range st.Integrations {
					if in.Provider == provider && string(in.Level) == level && in.Connected {
						if a.jsonOut {
							return a.printJSON(cmd, in)
						}
						fmt.Fprintf(cmd.OutOrStdout(), "%s is already connected at the %s level (%s).\n", provider, level, in.Account.Value)
						return nil
					}
				}
			} else {
				return err
			}

			connectURL := fmt.Sprintf("%s/integrations/%s/connect?level=%s", a.apiURL(), provider, level)
			errOut := cmd.ErrOrStderr()
			fmt.Fprintf(errOut, "Open this URL in the browser where you approved the CLI sign-in:\n  %s\n", connectURL)
			if err := browser.Open(connectURL); err == nil {
				fmt.Fprintln(errOut, "Opened your browser — finish the authorization there.")
			}
			if noWait {
				return nil
			}

			fmt.Fprintln(errOut, "Waiting for the connection…")
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(3 * time.Second):
				}
				st, err := a.integrationStatus(cmd)
				if err != nil {
					continue
				}
				for _, in := range st.Integrations {
					if in.Provider == provider && string(in.Level) == level && in.Connected {
						if a.jsonOut {
							return a.printJSON(cmd, in)
						}
						fmt.Fprintf(cmd.OutOrStdout(), "Connected %s (%s level): %s\n", provider, level, in.Account.Value)
						return nil
					}
				}
			}
			return fmt.Errorf("timed out waiting for %s to connect — check the browser flow or run `notifbuddy status`", provider)
		},
	}
	cmd.Flags().BoolVar(&userLevel, "user", false, "connect your personal account instead of the workspace")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "start the browser flow and exit without waiting")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "how long to wait for the connection")
	return cmd
}
