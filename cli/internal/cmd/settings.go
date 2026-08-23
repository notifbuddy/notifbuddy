package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"xolo/cli/internal/api"
)

func newSettingsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage Linear channel-creation configs",
		Long:  "Each config is scoped to one Linear team and controls when notifbuddy creates and archives a Slack channel for an issue, what the channel is named, and who is auto-added.",
	}
	cmd.AddCommand(
		newSettingsListCmd(a),
		newSettingsCreateCmd(a),
		newSettingsUpdateCmd(a),
		newSettingsDeleteCmd(a),
		newSettingsTestCmd(a),
	)
	return cmd
}

func (a *app) linearSettings(cmd *cobra.Command) (*api.LinearSettingsResponse, error) {
	c, err := a.api()
	if err != nil {
		return nil, err
	}
	res, err := c.GetLinearSettings(cmd.Context())
	return apiResult[api.LinearSettingsResponse](res, err)
}

func printSettings(cmd *cobra.Command, resp *api.LinearSettingsResponse) error {
	out := cmd.OutOrStdout()
	if !resp.Connected {
		fmt.Fprintln(out, "Linear is not connected at the workspace level — run `notifbuddy connect linear` first.")
	}
	teamName := map[string]string{}
	for _, t := range resp.Teams {
		teamName[t.TeamId] = t.TeamName
	}
	if len(resp.Configs) == 0 {
		fmt.Fprintln(out, "No configs yet. Create one with `notifbuddy settings create`.")
	} else {
		w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SETTING_ID\tTEAM\tCREATE\tTRIGGER\tNAME_TEMPLATE\tARCHIVE")
		for _, cfg := range resp.Configs {
			trigger := cfg.TriggerStatus.Value
			if cfg.CreationMode == "condition" {
				trigger = cfg.ConditionExpr.Value
			}
			archive := string(cfg.ArchiveMode.Value)
			if archive == "" {
				archive = "manual"
			}
			name := teamName[cfg.TeamId]
			if name == "" {
				name = cfg.TeamId
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				cfg.SettingId.Value, name, cfg.CreationMode, trigger, cfg.NameTemplate.Value, archive)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if len(resp.Teams) > 0 {
		fmt.Fprintln(out, "\nLinear teams (teamId — name — workflow states):")
		for _, t := range resp.Teams {
			fmt.Fprintf(out, "  %s — %s —", t.TeamId, t.TeamName)
			for _, s := range t.States {
				fmt.Fprintf(out, " %q", s.Name)
			}
			fmt.Fprintln(out)
		}
	}
	if len(resp.SlackMembers) > 0 {
		fmt.Fprintln(out, "\nSlack members (id — name):")
		for _, m := range resp.SlackMembers {
			kind := ""
			if m.IsBot {
				kind = " (bot)"
			}
			fmt.Fprintf(out, "  %s — %s%s\n", m.MemberId, m.Name, kind)
		}
	}
	if len(resp.SampleEvents) > 0 {
		fmt.Fprintln(out, "\nSample events for `settings test --sample`:")
		for _, s := range resp.SampleEvents {
			fmt.Fprintf(out, "  %s — %s\n", s.ID, s.Label)
		}
	}
	return nil
}

type settingsFlags struct {
	team             string
	creationMode     string
	triggerStatus    string
	nameTemplate     string
	condition        string
	archiveMode      string
	archiveStatus    string
	archiveCondition string
	autoAdd          []string
}

func (f *settingsFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&f.team, "team", "", "Linear team id this config applies to")
	fl.StringVar(&f.creationMode, "creation-mode", "", "when to create channels: status | condition | manual")
	fl.StringVar(&f.triggerStatus, "trigger-status", "", "workflow state name that triggers creation (status mode)")
	fl.StringVar(&f.nameTemplate, "name-template", "", "channel name template, e.g. 'tkt-${{ linear.issue.identifier }}'")
	fl.StringVar(&f.condition, "condition", "", "creation condition expression (condition mode)")
	fl.StringVar(&f.archiveMode, "archive-mode", "", "when to archive channels: status | condition | manual")
	fl.StringVar(&f.archiveStatus, "archive-status", "", "workflow state name that triggers archiving (status mode)")
	fl.StringVar(&f.archiveCondition, "archive-condition", "", "archive condition expression (condition mode)")
	fl.StringSliceVar(&f.autoAdd, "auto-add", nil, "Slack member ids to auto-add on creation (repeatable)")
}

func optStr(s string) api.OptString {
	if s == "" {
		return api.OptString{}
	}
	return api.NewOptString(s)
}

func (f *settingsFlags) apply(s *api.LinearSettings, changed func(string) bool) {
	if changed("team") {
		s.TeamId = f.team
	}
	if changed("creation-mode") {
		s.CreationMode = api.LinearSettingsCreationMode(f.creationMode)
	}
	if changed("trigger-status") {
		s.TriggerStatus = optStr(f.triggerStatus)
	}
	if changed("name-template") {
		s.NameTemplate = optStr(f.nameTemplate)
	}
	if changed("condition") {
		s.ConditionExpr = optStr(f.condition)
	}
	if changed("archive-mode") {
		if f.archiveMode == "" {
			s.ArchiveMode = api.OptLinearSettingsArchiveMode{}
		} else {
			s.ArchiveMode = api.NewOptLinearSettingsArchiveMode(api.LinearSettingsArchiveMode(f.archiveMode))
		}
	}
	if changed("archive-status") {
		s.ArchiveStatus = optStr(f.archiveStatus)
	}
	if changed("archive-condition") {
		s.ArchiveConditionExpr = optStr(f.archiveCondition)
	}
	if changed("auto-add") {
		s.AutoAddMembers = f.autoAdd
	}
	if s.AutoAddMembers == nil {
		s.AutoAddMembers = []string{}
	}
}

func newSettingsListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configs plus the teams, states, Slack members, and sample events you can reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resp, err := a.linearSettings(cmd)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, resp)
			}
			return printSettings(cmd, resp)
		},
	}
}

func newSettingsCreateCmd(a *app) *cobra.Command {
	f := &settingsFlags{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a channel-creation config for a Linear team",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			var s api.LinearSettings
			f.apply(&s, cmd.Flags().Changed)
			res, err := c.CreateLinearSettings(cmd.Context(), &s)
			resp, err := apiResult[api.LinearSettingsResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Config created.")
			return printSettings(cmd, resp)
		},
	}
	f.register(cmd)
	_ = cmd.MarkFlagRequired("team")
	_ = cmd.MarkFlagRequired("creation-mode")
	return cmd
}

func newSettingsUpdateCmd(a *app) *cobra.Command {
	f := &settingsFlags{}
	cmd := &cobra.Command{
		Use:   "update <settingId>",
		Short: "Update an existing config (only the flags you pass change)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := a.linearSettings(cmd)
			if err != nil {
				return err
			}
			var current *api.LinearSettings
			for i, cfg := range resp.Configs {
				if cfg.SettingId.Value == args[0] {
					current = &resp.Configs[i]
					break
				}
			}
			if current == nil {
				return fmt.Errorf("no config with settingId %q — run `notifbuddy settings list`", args[0])
			}
			f.apply(current, cmd.Flags().Changed)
			c, err := a.api()
			if err != nil {
				return err
			}
			res, err := c.UpdateLinearSettings(cmd.Context(), current, api.UpdateLinearSettingsParams{SettingId: args[0]})
			updated, err := apiResult[api.LinearSettingsResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, updated)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Config updated.")
			return printSettings(cmd, updated)
		},
	}
	f.register(cmd)
	return cmd
}

func newSettingsDeleteCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <settingId>",
		Short: "Delete a config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			res, err := c.DeleteLinearSettings(cmd.Context(), api.DeleteLinearSettingsParams{SettingId: args[0]})
			resp, err := apiResult[api.LinearSettingsResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, resp)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Config deleted.")
			return nil
		},
	}
}

func newSettingsTestCmd(a *app) *cobra.Command {
	f := &settingsFlags{}
	var sampleID, eventFile string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Dry-run a config against a sample or real event",
		Long:  "Renders the name template and evaluates the create/archive triggers against a built-in sample event (--sample, ids from `settings list`) or a raw event envelope JSON file (--event-file). Nothing is created.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			req := &api.TemplateTestRequest{
				NameTemplate:     optStr(f.nameTemplate),
				CreationMode:     optStr(f.creationMode),
				TriggerStatus:    optStr(f.triggerStatus),
				Condition:        optStr(f.condition),
				ArchiveMode:      optStr(f.archiveMode),
				ArchiveStatus:    optStr(f.archiveStatus),
				ArchiveCondition: optStr(f.archiveCondition),
				SampleId:         optStr(sampleID),
			}
			if eventFile != "" {
				raw, err := os.ReadFile(eventFile)
				if err != nil {
					return err
				}
				req.Event = api.NewOptString(string(raw))
			}
			res, err := c.TestLinearTemplate(cmd.Context(), req)
			result, err := apiResult[api.TemplateTestResponse](res, err)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(cmd, result)
			}
			out := cmd.OutOrStdout()
			if result.Error.Value != "" {
				fmt.Fprintf(out, "Template error: %s\n", result.Error.Value)
				return nil
			}
			fmt.Fprintf(out, "Rendered name:  %s\nWould create:   %v\nWould archive:  %v\n",
				result.Name, result.WouldCreate, result.WouldArchive)
			return nil
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&sampleID, "sample", "", "built-in sample event id (see `settings list`)")
	cmd.Flags().StringVar(&eventFile, "event-file", "", "path to a raw event envelope JSON file")
	cmd.MarkFlagsOneRequired("sample", "event-file")
	cmd.MarkFlagsMutuallyExclusive("sample", "event-file")
	return cmd
}
