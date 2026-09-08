package app

import (
	integrationTypes "github.com/jesseduffield/lazygit/pkg/integration/types"
)

// StartArgs is the struct that represents some things we want to do on program start
type StartArgs struct {
	// GitArg determines what context we open in
	GitArg GitArg
	// integration test (only relevant when invoking lazygit in the context of an integration test)
	IntegrationTest integrationTypes.IntegrationTest
	// FilterPath determines which path we're going to filter on so that we only see commits from that file.
	FilterPath string
	// ScreenMode determines the initial Screen Mode (normal, half or full) to use
	ScreenMode string
	// SideBySideDiffs, if true, overrides Gui.SideBySideDiffs for this run only
	// (see the --side-by-side CLI flag). Kept separate from UserConfig because
	// the user config gets reloaded from disk on every repo entry/switch and
	// whenever the config file changes, which would otherwise silently wipe an
	// in-memory-only override.
	SideBySideDiffs bool
}

type GitArg string

const (
	GitArgNone   GitArg = ""
	GitArgStatus GitArg = "status"
	GitArgBranch GitArg = "branch"
	GitArgLog    GitArg = "log"
	GitArgStash  GitArg = "stash"
)

func NewStartArgs(filterPath string, gitArg GitArg, screenMode string, sideBySideDiffs bool, test integrationTypes.IntegrationTest) StartArgs {
	return StartArgs{
		FilterPath:      filterPath,
		GitArg:          gitArg,
		ScreenMode:      screenMode,
		SideBySideDiffs: sideBySideDiffs,
		IntegrationTest: test,
	}
}
