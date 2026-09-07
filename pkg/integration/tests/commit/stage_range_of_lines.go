package commit

import (
	"github.com/jesseduffield/lazygit/pkg/config"
	. "github.com/jesseduffield/lazygit/pkg/integration/components"
)

var StageRangeOfLines = NewIntegrationTest(NewIntegrationTestArgs{
	Description:  "Staging a range of lines",
	ExtraCmdArgs: []string{},
	Skip:         false,
	SetupConfig: func(config *config.AppConfig) {
		config.GetUserConfig().Gui.UseHunkModeInStagingView = false
	},
	SetupRepo: func(shell *Shell) {
		shell.CreateFileAndAdd("myfile", "1st\n2nd\n3rd\n4th\n5th\n6th\n")
		shell.Commit("Add file")
		shell.UpdateFile("myfile", "1st changed\n2nd changed\n3rd\n4th\n5th changed\n6th\n")
	},
	Run: func(t *TestDriver, keys config.KeybindingConfig) {
		t.Views().Files().
			IsFocused().
			PressEnter()

		t.Views().Staging().
			Content(
				Contains("1   -1st\n2   -2nd\n  1 +1st changed\n  2 +2nd changed\n3 3  3rd\n4 4  4th\n5   -5th\n  5 +5th changed\n6 6  6th"),
			).
			SelectedLine(Equals("1   -1st")).
			Press(keys.Universal.ToggleRangeSelect).
			SelectNextItem().
			SelectNextItem().
			SelectNextItem().
			SelectNextItem().
			PressPrimaryAction().
			Content(
				Contains("3 3  3rd\n4 4  4th\n5   -5th\n  5 +5th changed\n6 6  6th"),
			).
			SelectedLine(Equals("5   -5th"))
	},
})
