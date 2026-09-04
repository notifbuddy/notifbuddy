package cmd

import (
	"fmt"
	"os"

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
		newSettingsValidateCmd(a),
	)
	return cmd
}

func testReqFromSettings(s *api.LinearSettings) *api.TemplateTestRequest {
	req := &api.TemplateTestRequest{
		NameTemplate:     s.NameTemplate,
		TopicTemplate:    s.TopicTemplate,
		CreationMode:     optStr(string(s.CreationMode)),
		TriggerStatus:    s.TriggerStatus,
		Condition:        s.ConditionExpr,
		ArchiveStatus:    s.ArchiveStatus,
		ArchiveCondition: s.ArchiveConditionExpr,
	}
	if s.ArchiveMode.Set {
		req.ArchiveMode = optStr(string(s.ArchiveMode.Value))
	}
	return req
}

func (f *settingsFlags) overlayTestReq(req *api.TemplateTestRequest, changed func(string) bool) {
	if changed("name-template") {
		req.NameTemplate = optStr(f.nameTemplate)
	}
	if changed("topic-template") {
		req.TopicTemplate = optStr(f.topicTemplate)
	}
	if changed("creation-mode") {
		req.CreationMode = optStr(f.creationMode)
	}
	if changed("trigger-status") {
		req.TriggerStatus = optStr(f.triggerStatus)
	}
	if changed("condition") {
		req.Condition = optStr(f.condition)
	}
	if changed("archive-mode") {
		req.ArchiveMode = optStr(f.archiveMode)
	}
	if changed("archive-status") {
		req.ArchiveStatus = optStr(f.archiveStatus)
	}
	if changed("archive-condition") {
		req.ArchiveCondition = optStr(f.archiveCondition)
	}
}

func findConfig(resp *api.LinearSettingsResponse, settingID string) (*api.LinearSettings, error) {
	for i, cfg := range resp.Configs {
		if cfg.SettingId.Value == settingID {
			return &resp.Configs[i], nil
		}
	}
	return nil, fmt.Errorf("no config with settingId %q — run `notifbuddy settings list`", settingID)
}

func (a *app) linearSettings(cmd *cobra.Command) (*api.LinearSettingsResponse, error) {
	c, err := a.api()
	if err != nil {
		return nil, err
	}
	res, err := c.GetLinearSettings(cmd.Context())
	return apiResult[api.LinearSettingsResponse](res, err)
}

type settingsFlags struct {
	team             string
	creationMode     string
	triggerStatus    string
	nameTemplate     string
	topicTemplate    string
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
	fl.StringVar(&f.topicTemplate, "topic-template", "", "channel topic template backlinking the Linear issue (empty = built-in default)")
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
	if changed("topic-template") {
		s.TopicTemplate = optStr(f.topicTemplate)
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
			return a.printJSON(cmd, resp)
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
			return a.printJSON(cmd, resp)
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
			current, err := findConfig(resp, args[0])
			if err != nil {
				return err
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
			return a.printJSON(cmd, updated)
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
			return a.printJSON(cmd, resp)
		},
	}
}

func newSettingsTestCmd(a *app) *cobra.Command {
	f := &settingsFlags{}
	var settingID, sampleID, eventFile string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Dry-run a config against a sample or real event",
		Long:  "Renders the name template and evaluates the create/archive triggers against a built-in sample event (--sample, ids from `settings list`) or a raw event envelope JSON file (--event-file). Nothing is created.\n\nThe config under test comes from --setting-id (a saved config), from the trigger flags, or both — flags override the saved config's fields.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.api()
			if err != nil {
				return err
			}
			req := &api.TemplateTestRequest{}
			if settingID != "" {
				resp, err := a.linearSettings(cmd)
				if err != nil {
					return err
				}
				cfg, err := findConfig(resp, settingID)
				if err != nil {
					return err
				}
				req = testReqFromSettings(cfg)
			}
			f.overlayTestReq(req, cmd.Flags().Changed)
			req.SampleId = optStr(sampleID)
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
			return a.printJSON(cmd, result)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&settingID, "setting-id", "", "test a saved config (id from `settings list`)")
	cmd.Flags().StringVar(&sampleID, "sample", "", "built-in sample event id (see `settings list`)")
	cmd.Flags().StringVar(&eventFile, "event-file", "", "path to a raw event envelope JSON file")
	cmd.MarkFlagsOneRequired("sample", "event-file")
	cmd.MarkFlagsMutuallyExclusive("sample", "event-file")
	return cmd
}

type validateSampleResult struct {
	SampleID     string `json:"sampleId"`
	Name         string `json:"name"`
	Topic        string `json:"topic"`
	WouldCreate  bool   `json:"wouldCreate"`
	WouldArchive bool   `json:"wouldArchive"`
	Error        string `json:"error,omitempty"`
}

type validateConfigResult struct {
	SettingID string                 `json:"settingId"`
	TeamID    string                 `json:"teamId"`
	TeamName  string                 `json:"teamName,omitempty"`
	Ok        bool                   `json:"ok"`
	Samples   []validateSampleResult `json:"samples"`
}

func newSettingsValidateCmd(a *app) *cobra.Command {
	var settingID string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Dry-run saved configs against every sample event and report template errors",
		Long:  "Runs each saved config (or just --setting-id) against every built-in sample event and reports the rendered name, trigger outcomes, and any template or condition errors. Exits non-zero if any config has an error, so harnesses can gate on it. Nothing is created.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resp, err := a.linearSettings(cmd)
			if err != nil {
				return err
			}
			configs := resp.Configs
			if settingID != "" {
				cfg, err := findConfig(resp, settingID)
				if err != nil {
					return err
				}
				configs = []api.LinearSettings{*cfg}
			}
			if len(configs) == 0 {
				return fmt.Errorf("no configs to validate — create one with `notifbuddy settings create`")
			}
			if len(resp.SampleEvents) == 0 {
				return fmt.Errorf("no sample events available — run `notifbuddy sync` first")
			}
			teamName := map[string]string{}
			for _, t := range resp.Teams {
				teamName[t.TeamId] = t.TeamName
			}
			c, err := a.api()
			if err != nil {
				return err
			}

			results := make([]validateConfigResult, 0, len(configs))
			failed := false
			for _, cfg := range configs {
				r := validateConfigResult{
					SettingID: cfg.SettingId.Value,
					TeamID:    cfg.TeamId,
					TeamName:  teamName[cfg.TeamId],
					Ok:        true,
				}
				for _, sample := range resp.SampleEvents {
					req := testReqFromSettings(&cfg)
					req.SampleId = optStr(sample.ID)
					res, err := c.TestLinearTemplate(cmd.Context(), req)
					result, err := apiResult[api.TemplateTestResponse](res, err)
					if err != nil {
						return err
					}
					r.Samples = append(r.Samples, validateSampleResult{
						SampleID:     sample.ID,
						Name:         result.Name,
						Topic:        result.Topic,
						WouldCreate:  result.WouldCreate,
						WouldArchive: result.WouldArchive,
						Error:        result.Error.Value,
					})
					if result.Error.Value != "" {
						r.Ok = false
						failed = true
					}
				}
				results = append(results, r)
			}

			if err := a.printJSON(cmd, results); err != nil {
				return err
			}
			if failed {
				return fmt.Errorf("validation found template errors")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&settingID, "setting-id", "", "validate only this saved config")
	return cmd
}
