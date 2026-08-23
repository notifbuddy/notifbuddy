package cmd

import (
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
			return a.printJSON(cmd, st)
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
