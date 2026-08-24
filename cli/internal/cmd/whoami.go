package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"xolo/cli/internal/api"
)

func newWhoamiCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in user and active organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			res, err := c.GetMe(cmd.Context())
			me, err := apiResult[api.UserResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, me)
			}
			out := cmd.OutOrStdout()
			name := strings.TrimSpace(me.FirstName.Value + " " + me.LastName.Value)
			if name != "" {
				fmt.Fprintf(out, "User:  %s <%s>\n", name, me.Email)
			} else {
				fmt.Fprintf(out, "User:  %s\n", me.Email)
			}
			if me.OrganizationId.Value == "" {
				fmt.Fprintln(out, "Org:   none active — run `notifbuddy org create <name>` or `notifbuddy org switch <name>`")
				return nil
			}
			orgName := me.OrganizationId.Value
			for _, o := range me.Organizations {
				if o.ID == me.OrganizationId.Value {
					orgName = o.Name
				}
			}
			fmt.Fprintf(out, "Org:   %s (%s)\n", orgName, me.OrganizationId.Value)
			if me.Role.Value != "" {
				fmt.Fprintf(out, "Role:  %s\n", me.Role.Value)
			}
			return nil
		},
	}
}
