package patch

import (
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/jesseduffield/generics/set"
	"github.com/jesseduffield/lazygit/pkg/gui/style"
	"github.com/jesseduffield/lazygit/pkg/theme"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestHighlightPatchUnknownFilename(t *testing.T) {
	hunks := []*Hunk{
		{
			oldStart: 1,
			newStart: 1,
			bodyLines: []*PatchLine{
				{Kind: ADDITION, Content: "+func foo() {}"},
			},
		},
	}

	assert.Equal(t, [][]highlightedLine{nil}, highlightPatch(hunks, ""))
	assert.Equal(t, [][]highlightedLine{nil}, highlightPatch(hunks, "file.some-unknown-extension-xyz"))
}

func spanTexts(line highlightedLine) []string {
	texts := make([]string, len(line))
	for i, span := range line {
		texts[i] = span.text
	}
	return texts
}

func styleOfText(t *testing.T, line highlightedLine, text string) style.TextStyle {
	t.Helper()
	for _, span := range line {
		if span.text == text {
			return span.style
		}
	}
	t.Fatalf("no span with text %q found in %v", text, spanTexts(line))
	return style.TextStyle{}
}

func TestHighlightHunkGoKeywordsAndIdentifiers(t *testing.T) {
	lexer := lexers.Match("file.go")
	assert.NotNil(t, lexer)

	hunk := &Hunk{
		oldStart: 1,
		newStart: 1,
		bodyLines: []*PatchLine{
			{Kind: ADDITION, Content: "+func foo() {}"},
		},
	}

	highlighted := highlightHunk(lexer, hunk)

	line := highlighted[0]
	assert.Equal(t, "func foo() {}", joinSpanTexts(line))

	assert.Equal(t, style.FgMagenta, styleOfText(t, line, "func"))
	// "foo" is the function being declared, so it's a NameFunction token
	assert.Equal(t, style.FgBlue, styleOfText(t, line, "foo"))
	assert.Equal(t, theme.DefaultTextColor, styleOfText(t, line, "("))
}

func joinSpanTexts(line highlightedLine) string {
	result := ""
	for _, span := range line {
		result += span.text
	}
	return result
}

func TestHighlightHunkMultilineConstructSpansContextLines(t *testing.T) {
	lexer := lexers.Match("file.go")
	assert.NotNil(t, lexer)

	hunk := &Hunk{
		oldStart: 1,
		newStart: 1,
		bodyLines: []*PatchLine{
			{Kind: CONTEXT, Content: " /*"},
			{Kind: CONTEXT, Content: " a comment"},
			{Kind: CONTEXT, Content: " */"},
		},
	}

	highlighted := highlightHunk(lexer, hunk)

	// the block comment spans three physical lines, but is a single token as
	// far as the lexer is concerned; each physical line's reconstructed
	// content should still be colored as a comment.
	assert.Equal(t, style.FgBlackLighter, styleOfText(t, highlighted[0], "/*"))
	assert.Equal(t, style.FgBlackLighter, styleOfText(t, highlighted[1], "a comment"))
	assert.Equal(t, style.FgBlackLighter, styleOfText(t, highlighted[2], "*/"))
}

func TestFormatViewFallsBackWhenHighlightingUnavailable(t *testing.T) {
	patch := Parse(simpleDiff) // filename left unset

	withHighlighting := utils.Decolorise(patch.FormatView(FormatViewOpts{IncLineIndices: set.New[int]()}))
	patch2 := Parse(simpleDiff).SetFilename("file.some-unknown-extension-xyz")
	withoutMatch := utils.Decolorise(patch2.FormatView(FormatViewOpts{IncLineIndices: set.New[int]()}))

	assert.Equal(t, withHighlighting, withoutMatch)
}
