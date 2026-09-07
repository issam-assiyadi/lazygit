package gui

import (
	"testing"

	appTypes "github.com/jesseduffield/lazygit/pkg/app/types"
	"github.com/jesseduffield/lazygit/pkg/config"
	"github.com/stretchr/testify/assert"
)

// Regression test for a real bug: the --side-by-side CLI flag was applied to
// UserConfig once at startup, but gui.onNewRepo unconditionally reloads
// UserConfig from disk (via ReloadUserConfigForRepo/ReloadChangedUserConfigFiles)
// before the user ever sees anything rendered - silently discarding the
// in-memory-only override. applySideBySideDiffsOverride is called again after
// every such reload specifically to survive this; verify it actually does.
func TestApplySideBySideDiffsOverride(t *testing.T) {
	appConfig := config.NewDummyAppConfig()
	// simulates what a reload-from-disk just did: replaced UserConfig with
	// whatever's actually on disk, which never had the flag written to it
	appConfig.GetUserConfig().Gui.SideBySideDiffs = false

	applySideBySideDiffsOverride(appTypes.StartArgs{SideBySideDiffs: true}, appConfig)

	assert.True(t, appConfig.GetUserConfig().Gui.SideBySideDiffs)
}

func TestApplySideBySideDiffsOverrideNoopWhenFlagNotPassed(t *testing.T) {
	appConfig := config.NewDummyAppConfig()
	appConfig.GetUserConfig().Gui.SideBySideDiffs = false

	applySideBySideDiffsOverride(appTypes.StartArgs{SideBySideDiffs: false}, appConfig)

	assert.False(t, appConfig.GetUserConfig().Gui.SideBySideDiffs)
}
