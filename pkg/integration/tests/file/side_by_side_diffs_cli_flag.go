package file

import (
	"github.com/jesseduffield/lazygit/pkg/config"
	. "github.com/jesseduffield/lazygit/pkg/integration/components"
)

// Smoke test that the --side-by-side CLI flag reaches real flag parsing and
// turns on the side-by-side preview end to end. Note: this alone can't catch
// the reload-wipes-the-override bug that motivated this flag's current
// implementation (see TestApplySideBySideDiffsOverride in pkg/gui/gui_test.go)
// - integration tests call AppConfig.SaveGlobalUserConfig after SetupConfig,
// which persists whatever's in memory to the test's config file, so the
// later reload-from-disk in gui.onNewRepo reads back the same value
// regardless of whether the override actually survives that reload for a
// real (non-test) run. The unit test is what actually verifies that.
var SideBySideDiffsCliFlag = NewIntegrationTest(NewIntegrationTestArgs{
	Description:  "The --side-by-side CLI flag turns on the side-by-side diff preview",
	ExtraCmdArgs: []string{"--side-by-side"},
	Skip:         false,
	SetupConfig: func(config *config.AppConfig) {
	},
	SetupRepo: func(shell *Shell) {
		shell.CreateFileAndAdd("file1", "one\ntwo\nthree\n")
		shell.Commit("first commit")

		shell.UpdateFile("file1", "one\nTWO\nthree\n")
	},
	Run: func(t *TestDriver, keys config.KeybindingConfig) {
		t.Views().Files().
			IsFocused().
			Lines(
				Contains("M file1").IsSelected(),
			)

		t.Views().Main().
			Content(Contains("│")).
			Content(DoesNotContain("+TWO")).
			Content(Contains("TWO"))
	},
})
