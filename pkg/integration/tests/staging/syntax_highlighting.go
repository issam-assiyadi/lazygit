package staging

import (
	"github.com/jesseduffield/lazygit/pkg/config"
	. "github.com/jesseduffield/lazygit/pkg/integration/components"
)

var SyntaxHighlighting = NewIntegrationTest(NewIntegrationTestArgs{
	Description:  "Highlights the syntax of a recognized file's content in the staging view",
	ExtraCmdArgs: []string{},
	Skip:         false,
	SetupConfig:  func(config *config.AppConfig) {},
	SetupRepo: func(shell *Shell) {
		shell.CreateFileAndAdd("main.go", "package main\n\nfunc main() {\n}\n")
		shell.Commit("one")

		shell.UpdateFile("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	},
	Run: func(t *TestDriver, keys config.KeybindingConfig) {
		t.Views().Files().
			IsFocused().
			Lines(
				Contains("main.go").IsSelected(),
			).
			PressEnter()

		t.Views().Staging().
			IsFocused().
			Content(Contains(`println("hello")`)).
			// hex, not color names: our basic ANSI styles render as palette
			// colors, which ContainsColoredText only matches against an RGB
			// color parsed from a hex string, not a color name
			ContainsColoredText("#800080", "func").
			ContainsColoredText("#808000", `"hello"`)
	},
})
