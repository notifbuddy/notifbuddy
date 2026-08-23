package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"xolo/cli/internal/api"
)

func newStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show integration connection status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.integrationStatus(cmd)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, st)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "PROVIDER\tLEVEL\tCONNECTED\tACCOUNT")
			for _, in := range st.Integrations {
				connected := "no"
				if in.Connected {
					connected = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", in.Provider, in.Level, connected, in.Account.Value)
			}
			return w.Flush()
		},
	}
}

func (a *app) integrationStatus(cmd *cobra.Command) (*api.IntegrationStatusResponse, error) {
	c, err := a.api()
	if err != nil {
		return nil, err
	}
	res, err := c.GetIntegrationStatus(cmd.Context())
	return apiResult[api.IntegrationStatusResponse](res, err)
}
