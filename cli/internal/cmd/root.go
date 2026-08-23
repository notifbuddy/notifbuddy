package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"xolo/cli/internal/api"
	"xolo/cli/internal/authd"
	"xolo/cli/internal/config"
	"xolo/cli/internal/tokenstore"
)

var Version = "dev"

type app struct {
	v       *viper.Viper
	jsonOut bool
	cfgFile string
}

func Execute(ctx context.Context) int {
	a := &app{v: viper.New()}
	root := newRootCmd(a)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "notifbuddy:", err)
		if errors.Is(err, tokenstore.ErrNotLoggedIn) {
			return 1
		}
		return 1
	}
	return 0
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "notifbuddy",
		Short:         "notifbuddy CLI — two-way Linear <-> Slack sync, from your terminal",
		Long:          "notifbuddy connects Linear and Slack and keeps issues and channels in sync.\nUse it to sign in, connect integrations, and manage channel-creation configs.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return config.Init(a.v, a.cfgFile)
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&a.cfgFile, "config", "", "config file (default ~/.config/notifbuddy/config.yaml)")
	pf.String("api-url", config.DefaultAPIURL, "notifbuddy API base URL")
	pf.String("auth-url", config.DefaultAuthURL, "notifbuddy auth service base URL")
	pf.String("dashboard-url", config.DefaultDashboardURL, "notifbuddy dashboard base URL")
	pf.BoolVar(&a.jsonOut, "json", false, "machine-readable JSON output")
	_ = a.v.BindPFlag("api_url", pf.Lookup("api-url"))
	_ = a.v.BindPFlag("auth_url", pf.Lookup("auth-url"))
	_ = a.v.BindPFlag("dashboard_url", pf.Lookup("dashboard-url"))

	root.AddCommand(
		newLoginCmd(a),
		newLogoutCmd(a),
		newWhoamiCmd(a),
		newOrgCmd(a),
		newStatusCmd(a),
		newConnectCmd(a),
		newSettingsCmd(a),
		newSyncCmd(a),
		newWebhooksCmd(a),
		newSkillCmd(a),
	)
	return root
}

func (a *app) apiURL() string       { return config.Trim(a.v.GetString("api_url")) }
func (a *app) authURL() string      { return config.Trim(a.v.GetString("auth_url")) }
func (a *app) dashboardURL() string { return config.Trim(a.v.GetString("dashboard_url")) }

func (a *app) authd() *authd.Client { return authd.New(a.authURL()) }

func (a *app) token() (string, error) { return tokenstore.Load(a.authURL()) }

type bearerDoer struct {
	token string
	hc    *http.Client
}

func (d bearerDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+d.token)
	return d.hc.Do(req)
}

func (a *app) api() (*api.Client, error) {
	token, err := a.token()
	if err != nil {
		return nil, err
	}
	return api.NewClient(a.apiURL(), api.WithClient(bearerDoer{token: token, hc: http.DefaultClient}))
}

func apiResult[T any](res any, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	if v, ok := res.(*T); ok {
		return v, nil
	}
	if raw, mErr := json.Marshal(res); mErr == nil {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Message != "" {
			return nil, errors.New(e.Message)
		}
	}
	return nil, fmt.Errorf("unexpected API response (%T)", res)
}

func (a *app) printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
