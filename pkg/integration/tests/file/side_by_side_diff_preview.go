package file

import (
	"github.com/jesseduffield/lazygit/pkg/config"
	. "github.com/jesseduffield/lazygit/pkg/integration/components"
)

var SideBySideDiffPreview = NewIntegrationTest(NewIntegrationTestArgs{
	Description:  "The Files panel's passive diff preview renders side-by-side (with per-column line numbers) when Gui.SideBySideDiffs is on, for a single selected file, but falls back to the unified renderer for a selected directory",
	ExtraCmdArgs: []string{},
	Skip:         false,
	SetupConfig: func(config *config.AppConfig) {
		config.GetUserConfig().Gui.SideBySideDiffs = true
	},
	SetupRepo: func(shell *Shell) {
		shell.CreateFileAndAdd("dir/file1", "one\ntwo\nthree\n")
		shell.Commit("first commit")

		shell.UpdateFile("dir/file1", "one\nTWO\nthree\n")
	},
	Run: func(t *TestDriver, keys config.KeybindingConfig) {
		t.Views().Files().
			IsFocused().
			Lines(
				Contains("▼ dir").IsSelected(),
				Contains("M file1"),
			).
			// the file's own diff renders side-by-side: old content on the
			// left, the divider, and the changed content on the right -
			// without the '+'/'-' marker glyphs the unified view would show
			NavigateToLine(Contains("file1"))

		t.Views().Main().
			Content(Contains("│")).
			Content(DoesNotContain("+TWO")).
			Content(Contains("TWO")).
			// each column gets its own line-number gutter
			Content(Contains("1 one")).
			Content(Contains("3 three"))

		// selecting the parent directory diffs multiple files at once,
		// which isn't representable as a single patch.Patch - this must
		// fall back to the unified renderer regardless of the config
		t.Views().Files().
			NavigateToLine(Contains("dir"))

		t.Views().Main().
			Content(Contains("+TWO"))
	},
})
