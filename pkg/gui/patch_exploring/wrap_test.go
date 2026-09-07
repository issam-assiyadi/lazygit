package patch_exploring

import (
	"strings"
	"testing"

	"github.com/jesseduffield/lazygit/pkg/commands/patch"
	"github.com/jesseduffield/lazygit/pkg/gocui"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/stretchr/testify/assert"
)

// Regression test for a bug where the line-number gutter (which is only
// prefixed once at the start of each logical line, not repeated on wrapped
// continuation rows) caused this package's row-count bookkeeping to
// disagree with how the view actually wraps the rendered content: the
// bookkeeping shrank the wrap width by the gutter width for every row,
// while only the first row of a wrapped line actually loses that width.
// Also exercises the padded-to-width hunk header (the header's background
// band is padded out to the view width but must still be treated as a
// single, unwrapped row).
func TestWrapPatchLinesMatchesActualRendering(t *testing.T) {
	longLine := strings.Repeat("x", 70)
	diffText := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-short old line\n+" +
		longLine + "\n context2\n"

	p := patch.Parse(diffText)
	gutterWidth := p.GutterWidth(true)
	if gutterWidth <= 0 {
		t.Fatalf("expected a positive gutter width, got %d", gutterWidth)
	}

	view := gocui.NewView("test", 0, 0, 21, 10, gocui.OutputNormal)
	view.Wrap = true

	_, bookkeepingPatchLineIndices := wrapPatchLines(p, gutterWidth, view)

	rendered := utils.Decolorise(p.FormatView(patch.FormatViewOpts{ShowLineNumbers: true, Width: view.InnerWidth()}))
	_, _, actualPatchLineIndices := utils.WrapViewLinesToWidth(
		true, view.Editable, strings.TrimSuffix(rendered, "\n"), view.InnerWidth(), view.TabWidth)

	assert.Equal(t, len(actualPatchLineIndices), len(bookkeepingPatchLineIndices),
		"bookkeeping row count must match how the rendered (gutter-prefixed) content actually wraps")
}
