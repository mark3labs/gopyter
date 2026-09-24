//go:build noai

package main

import (
	"errors"
	"io"
	"strings"

	"github.com/mark3labs/gopyter/internal/config"
	"github.com/mark3labs/gopyter/internal/ui"
	"github.com/spf13/cobra"
)

// errNoAI is reported when AI is requested from a build without it.
var errNoAI = errors.New("this gopyter was built without AI support (-tags noai)")

// newAI returns nil: the UI then shows no AI feature at all.
func newAI(string, bool) *ui.AIConfig { return nil }

// resolveModel reports AI as off. Asking for a model explicitly is an
// error; a saved one is ignored, so a shared config works with both builds.
func resolveModel(cmd *cobra.Command, flag string, _ config.Settings) (string, bool, error) {
	if cmd.Flags().Changed("model") && !strings.EqualFold(strings.TrimSpace(flag), "off") && strings.TrimSpace(flag) != "" {
		return "", false, errNoAI
	}
	return "", false, nil
}

func modelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "model",
		Short: "Show or set the AI model (not available in this build)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), errNoAI.Error()+"\n")
			return err
		},
	}
}
