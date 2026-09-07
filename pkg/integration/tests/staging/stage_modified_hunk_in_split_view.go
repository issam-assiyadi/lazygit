package staging

import (
	"fmt"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/config"
	. "github.com/jesseduffield/lazygit/pkg/integration/components"
)

var StageModifiedHunkInSplitView = NewIntegrationTest(NewIntegrationTestArgs{
	Description:  "Staging a whole hunk containing a modified line in split diff view stages both the deletion and the addition, not just whichever column the cursor happens to be on",
	ExtraCmdArgs: []string{},
	Skip:         false,
	SetupConfig: func(config *config.AppConfig) {
		config.GetUserConfig().Gui.UseSplitDiffInStagingView = true
	},
	SetupRepo: func(shell *Shell) {
		lines := make([]string, 0, 30)
		for i := range 30 {
			lines = append(lines, fmt.Sprintf("line%d", i))
		}
		shell.CreateFileAndAdd("file1", strings.Join(lines, "\n")+"\n")
		shell.Commit("one")

		lines[2] = "line2-changed"
		lines[3] = "line3-changed"
		shell.UpdateFile("file1", strings.Join(lines, "\n")+"\n")
	},
	Run: func(t *TestDriver, keys config.KeybindingConfig) {
		t.Views().Files().
			IsFocused().
			Lines(
				Contains("file1").IsSelected(),
			).
			PressEnter()

		t.Views().Staging().
			IsFocused().
			Content(Contains("line2-changed")).
			// hunk-select mode is on by default: the cursor starts on the
			// old (left) column, so staging here must not silently stage
			// only the deletions.
			PressPrimaryAction()

		t.Views().StagingSecondary().
			// both halves of the modified block must have been staged
			// together: if HUNK selection were column-locked, only the
			// deletions would have moved over here, and the hunk header
			// would show a mismatched old/new line count.
			Content(Contains("@@ -1,7 +1,7 @@")).
			Content(Contains("line2-changed")).
			Content(Contains("line3-changed"))

		t.Views().Staging().
			// the addition half must be gone from the unstaged view too -
			// not left behind as an orphaned, unpaired change.
			Content(DoesNotContain("line2-changed")).
			Content(DoesNotContain("line3-changed"))
	},
})
