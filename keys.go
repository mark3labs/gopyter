package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/mark3labs/gopyter/internal/config"
	"github.com/mark3labs/gopyter/internal/ui"
	"github.com/spf13/cobra"
)

func keysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keys",
		Short: "List current and default key bindings",
		Long: "List commands, modes, current keys, defaults and descriptions. * marks overrides.\n\n" +
			"Configure replacements in config.json, for example:\n" +
			`  {"keys":{"run_all":["ctrl+a"],"quit":[]}}`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings := loadSettings(cmd)
			commands, warnings := ui.KeyCommands(settings.Keys)
			for _, w := range warnings {
				cmd.PrintErrf("gopyter: key bindings: %s\n", w)
			}
			var b strings.Builder
			b.WriteString("  COMMAND\tMODE\tKEYS\tDEFAULT\tDESCRIPTION\n")
			for _, c := range commands {
				mark := " "
				if _, ok := settings.Keys[c.Name]; ok {
					mark = "*"
				}
				keys := strings.Join(c.Keys, " ")
				if keys == "" {
					keys = "-"
				}
				fmt.Fprintf(&b, "%s %s\t%s\t%s\t%s\t%s\n", mark, c.Name, c.Mode, keys, strings.Join(c.Defaults, " "), c.Desc)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			if _, err := w.Write([]byte(b.String())); err != nil {
				return err
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if path, err := config.Path(); err == nil {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "\nsettings: %s\n", path)
				return err
			}
			return nil
		},
	}
}
