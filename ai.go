//go:build !noai

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/mark3labs/gopyter/internal/ai"
	"github.com/mark3labs/gopyter/internal/config"
	"github.com/mark3labs/gopyter/internal/ui"
	"github.com/spf13/cobra"
)

// newAI configures the AI features for the UI. model is the selected
// model, kept while AI is off; on says whether AI is on.
func newAI(model string, on bool) *ui.AIConfig {
	return &ui.AIConfig{
		Model:       model,
		On:          on && model != "",
		New:         func(m string) ui.Assistant { return ai.New(m) },
		Models:      ai.Catalog,
		LocalModels: ai.OllamaModels,
		Save:        saveAI,
	}
}

// saveAI persists the model picked in the UI and whether AI is on.
func saveAI(model string, on bool) error {
	return config.Update(func(s *config.Settings) { s.AIModel, s.AIOff = model, !on })
}

// resolveModel picks the AI model and whether AI is on: the --model flag
// for this session, else the saved setting. A bad flag is an error; a bad
// saved value only a warning, so a stale config never prevents gopyter
// from starting.
func resolveModel(cmd *cobra.Command, flag string, s config.Settings) (string, bool, error) {
	saved, err := ai.Resolve(s.AIModel)
	if err != nil {
		cmd.PrintErrf("gopyter: ignoring saved AI model: %v\n", err)
		saved = ""
	}
	if cmd.Flags().Changed("model") {
		m, err := ai.Resolve(flag)
		if err != nil {
			return "", false, fmt.Errorf("--model: %w", err)
		}
		if m == "" { // --model off: off for this session, keep the saved model
			return saved, false, nil
		}
		return m, true, nil
	}
	return saved, saved != "" && !s.AIOff, nil
}

func modelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "model [provider | provider/model | off]",
		Short: "Show or set the AI model (AI features are off until you set one)",
		Long: "AI features, like changing a cell with e or fixing a failing one with f, are off\n" +
			"until you pick a model, here or with M inside gopyter.\n" +
			"Without an argument, this shows the current setting and which providers have an\n" +
			"API key in the environment. With one, it saves the model: a provider name picks\n" +
			"a default model for it, provider/model picks any model kit supports, and off\n" +
			"turns AI features off again (the model is remembered).\n\n" +
			"API keys are read from the provider's environment variable (like ANTHROPIC_API_KEY)\n" +
			"and never stored by gopyter. --model overrides the setting for one session.",
		Example: "  gopyter model\n  gopyter model anthropic\n  gopyter model openai/gpt-5.6\n  gopyter model ollama/qwen3-coder\n  gopyter model off",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if len(args) == 0 {
				s := loadSettings(cmd)
				current, err := ai.Resolve(s.AIModel)
				if err != nil {
					cmd.PrintErrf("gopyter: saved AI model: %v\n", err)
				}
				_, err = io.WriteString(w, modelStatus(current, current != "" && !s.AIOff))
				return err
			}
			m, err := ai.Resolve(args[0])
			if err != nil {
				return err
			}
			var b strings.Builder
			if m == "" {
				// Off keeps the model, so M in gopyter can turn it back on.
				if err := config.Update(func(s *config.Settings) { s.AIOff = true }); err != nil {
					return err
				}
				b.WriteString("AI features are off. Turn them back on with M in gopyter, or gopyter model <model>.\n")
				_, err = io.WriteString(w, b.String())
				return err
			}
			if err := saveAI(m, true); err != nil {
				return err
			}
			b.WriteString("AI model: ")
			b.WriteString(m)
			b.WriteString("\n")
			if warn := ai.Warning(m); warn != "" {
				b.WriteString("note: ")
				b.WriteString(warn)
				b.WriteString("\n")
			}
			if err := ai.Ready(m); err != nil {
				b.WriteString("warning: ")
				b.WriteString(err.Error())
				b.WriteString("\n")
			}
			b.WriteString("\nPress e on a code cell to have it written or changed as you describe, or f on a\n")
			b.WriteString("cell with an error to have it fixed. A request sends that cell, its error and\n")
			b.WriteString("the code cells above it (not their outputs) to the provider.\n")
			b.WriteString("Press M in gopyter to switch models or turn AI off.\n")
			_, err = io.WriteString(w, b.String())
			return err
		},
	}
}

// modelStatus describes the AI setting and the available providers.
func modelStatus(current string, on bool) string {
	var b strings.Builder
	switch {
	case current == "":
		b.WriteString("AI features are off.\n")
	case !on:
		b.WriteString("AI features are off (model: ")
		b.WriteString(current)
		b.WriteString(").\n")
	default:
		b.WriteString("AI model: ")
		b.WriteString(current)
		if err := ai.Ready(current); err != nil {
			b.WriteString(" (")
			b.WriteString(err.Error())
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nProviders (gopyter model <provider>):\n")
	w, mw := 0, 0
	for _, p := range ai.Providers {
		w, mw = max(w, len(p.ID)), max(mw, len(p.Model))
	}
	for _, p := range ai.Providers {
		full := p.ID + "/" + p.Model
		mark := "  "
		if full == current && on {
			mark = "* "
		}
		b.WriteString(mark)
		b.WriteString(p.ID)
		b.WriteString(strings.Repeat(" ", w-len(p.ID)+2))
		b.WriteString(p.Model)
		b.WriteString(strings.Repeat(" ", mw-len(p.Model)+2))
		if ai.Ready(full) == nil {
			b.WriteString("key found")
		} else {
			b.WriteString("needs ")
			b.WriteString(ai.EnvHint(p.ID))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nAny provider/model kit supports works too, e.g. ollama/qwen3-coder for a local model.\n")
	if p, err := config.Path(); err == nil {
		b.WriteString("settings: ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	return b.String()
}
