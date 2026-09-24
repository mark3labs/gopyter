// Package ai provides gopyter's optional AI assistance, built on the kit
// SDK (github.com/mark3labs/kit). Nothing in it runs until the user picks a
// model: without one, gopyter never creates an agent or contacts a provider.
//
// Agents are isolated (kit.NewIsolatedAgent): they don't load .kit.yml,
// AGENTS.md, skills, extensions or MCP servers from the working directory
// or the user's kit setup, and they have no shell or file tools. They only
// get the tools this package gives them, which compile code without running
// it. Credentials come from the provider's environment variable, or from
// kit's credential store for users who ran "kit auth login".
//
// Building with -tags noai leaves out kit and everything that uses it; only
// the types in this file remain, so the UI compiles unchanged.
package ai

import (
	"context"

	"github.com/mark3labs/gopyter/internal/kernel"
)

// Off is the model setting that disables AI features.
const Off = "off"

// Checker compiles a cell without running it. *kernel.Kernel implements it.
type Checker interface {
	Check(ctx context.Context, cellID, name, src string) (kernel.CheckResult, error)
}

// Cell is a notebook cell shown to the model for context.
type Cell struct {
	Name   string // as in error positions, e.g. "In[2]"
	Source string
}

// Request describes a cell for the model to change: a failing cell to fix,
// or any code cell to edit as Instruction says.
type Request struct {
	CellID string
	Name   string // as in error positions, e.g. "In[3]"
	Source string // may be empty when editing: the model writes the cell
	// Error is the error output of the cell's last run, if it failed.
	Error string
	// Instruction is what the user asked for. Fixes don't have one.
	Instruction string
	// Before are the code cells above this one, oldest first.
	Before []Cell
	// Declarations are the identifiers the kernel currently knows.
	Declarations []string
	GoVersion    string
}

// Proposal is a new version of a cell proposed by the model.
type Proposal struct {
	Source      string
	Explanation string
	// Missing lists imported packages whose modules aren't downloaded
	// yet, so the proposal is only checked up to its imports. Running the
	// cell downloads them.
	Missing []string
}

// OllamaProvider is the provider id of local Ollama models. Kit's catalog
// has no models for it: they are whatever the local server has installed.
const OllamaProvider = "ollama"

// ModelEntry is a model offered by the model picker.
type ModelEntry struct {
	Provider string
	Model    string // without the provider prefix
	Name     string // human-friendly name, may be empty
	// Ready reports whether the provider's credentials are available.
	// Otherwise Missing names the environment variable(s) to set.
	Ready   bool
	Missing string
	// Recommended marks gopyter's default model for its provider.
	Recommended bool
}

// ID returns the "provider/model" string.
func (e ModelEntry) ID() string { return e.Provider + "/" + e.Model }
