package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	cli "xolo/cli"
)

const skillName = "notifbuddy-onboarding"

func newSkillCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install or print the onboarding skill for coding agents",
		Long:  "The bundled skill teaches Claude, Codex, and other agents how to run notifbuddy onboarding with this CLI. Install it once, then ask your agent to \"set up notifbuddy\".",
	}
	cmd.AddCommand(newSkillInstallCmd(a), newSkillShowCmd(a))
	return cmd
}

func agentSkillDirs(home string) map[string]string {
	return map[string]string{
		"claude": filepath.Join(home, ".claude", "skills"),
		"codex":  filepath.Join(home, ".codex", "skills"),
	}
}

func newSkillInstallCmd(a *app) *cobra.Command {
	var agents []string
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the onboarding skill into agent skill directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var targets []string
			if dir != "" {
				targets = append(targets, dir)
			} else {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				dirs := agentSkillDirs(home)
				for _, agent := range agents {
					d, ok := dirs[agent]
					if !ok {
						return fmt.Errorf("unknown agent %q (want claude or codex, or use --dir)", agent)
					}
					targets = append(targets, d)
				}
			}
			installed := make([]string, 0, len(targets))
			for _, t := range targets {
				dest := filepath.Join(t, skillName)
				if err := copySkill(dest); err != nil {
					return fmt.Errorf("install to %s: %w", dest, err)
				}
				installed = append(installed, dest)
			}
			if a.jsonOut {
				return a.printJSON(cmd, map[string]any{"installed": installed})
			}
			out := cmd.OutOrStdout()
			for _, p := range installed {
				fmt.Fprintf(out, "Installed skill: %s\n", p)
			}
			fmt.Fprintln(out, "Now ask your agent to \"set up notifbuddy\".")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&agents, "agent", []string{"claude"}, "agents to install for: claude, codex (repeatable)")
	cmd.Flags().StringVar(&dir, "dir", "", "install into this skills directory instead of an agent default")
	return cmd
}

func newSkillShowCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the onboarding skill to stdout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := cli.SkillsFS.ReadFile("skills/" + skillName + "/SKILL.md")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(raw)
			return err
		},
	}
}

func copySkill(dest string) error {
	src := "skills/" + skillName
	return fs.WalkDir(cli.SkillsFS, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := cli.SkillsFS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
}
