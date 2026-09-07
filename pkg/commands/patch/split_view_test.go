package patch

import (
	"testing"

	"github.com/jesseduffield/lazygit/pkg/gui/style"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func idxPtr(i int) *int { return &i }

func TestComputeHunkSplitRowsContextOnly(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: CONTEXT, Content: " a"},
		{Kind: CONTEXT, Content: " b"},
	}

	rows := computeHunkSplitRows(bodyLines)

	assert.Equal(t, []splitRow{
		{old: idxPtr(0), new: idxPtr(0)},
		{old: idxPtr(1), new: idxPtr(1)},
	}, rows)
}

func TestComputeHunkSplitRowsEqualModificationBlock(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: CONTEXT, Content: " commit  string"},
		{Kind: DELETION, Content: "-date    string"},
		{Kind: ADDITION, Content: "+date1   string"},
		{Kind: CONTEXT, Content: " version string"},
	}

	rows := computeHunkSplitRows(bodyLines)

	assert.Equal(t, []splitRow{
		{old: idxPtr(0), new: idxPtr(0)},
		{old: idxPtr(1), new: idxPtr(2)},
		{old: idxPtr(3), new: idxPtr(3)},
	}, rows)
}

func TestComputeHunkSplitRowsMoreDeletionsThanAdditions(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: DELETION, Content: "-one"},
		{Kind: DELETION, Content: "-two"},
		{Kind: DELETION, Content: "-three"},
		{Kind: ADDITION, Content: "+only"},
	}

	rows := computeHunkSplitRows(bodyLines)

	assert.Equal(t, []splitRow{
		{old: idxPtr(0), new: idxPtr(3)},
		{old: idxPtr(1)},
		{old: idxPtr(2)},
	}, rows)
}

func TestComputeHunkSplitRowsMoreAdditionsThanDeletions(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: DELETION, Content: "-only"},
		{Kind: ADDITION, Content: "+one"},
		{Kind: ADDITION, Content: "+two"},
		{Kind: ADDITION, Content: "+three"},
	}

	rows := computeHunkSplitRows(bodyLines)

	assert.Equal(t, []splitRow{
		{old: idxPtr(0), new: idxPtr(1)},
		{new: idxPtr(2)},
		{new: idxPtr(3)},
	}, rows)
}

func TestComputeHunkSplitRowsPureAdditionBlock(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: CONTEXT, Content: " unrelated"},
		{Kind: ADDITION, Content: "+new line one"},
		{Kind: ADDITION, Content: "+new line two"},
	}

	rows := computeHunkSplitRows(bodyLines)

	assert.Equal(t, []splitRow{
		{old: idxPtr(0), new: idxPtr(0)},
		{new: idxPtr(1)},
		{new: idxPtr(2)},
	}, rows)
}

func TestComputeHunkSplitRowsMultipleBlocksInOneHunk(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: DELETION, Content: "-foo"},
		{Kind: ADDITION, Content: "+foot"},
		{Kind: CONTEXT, Content: " unrelated"},
		{Kind: DELETION, Content: "-bar"},
		{Kind: ADDITION, Content: "+baz"},
	}

	rows := computeHunkSplitRows(bodyLines)

	assert.Equal(t, []splitRow{
		{old: idxPtr(0), new: idxPtr(1)},
		{old: idxPtr(2), new: idxPtr(2)},
		{old: idxPtr(3), new: idxPtr(4)},
	}, rows)
}

func TestComputeSplitRowsIndexedByHunk(t *testing.T) {
	hunks := []*Hunk{
		{bodyLines: []*PatchLine{{Kind: CONTEXT, Content: " a"}}},
		{bodyLines: []*PatchLine{{Kind: ADDITION, Content: "+b"}}},
	}

	rows := computeSplitRows(hunks)

	assert.Equal(t, [][]splitRow{
		{{old: idxPtr(0), new: idxPtr(0)}},
		{{new: idxPtr(0)}},
	}, rows)
}

func TestHunkLineNumbers(t *testing.T) {
	hunk := &Hunk{
		oldStart: 8,
		newStart: 20,
		bodyLines: []*PatchLine{
			{Kind: CONTEXT, Content: " a"},
			{Kind: DELETION, Content: "-b"},
			{Kind: ADDITION, Content: "+c"},
			{Kind: ADDITION, Content: "+d"},
			{Kind: CONTEXT, Content: " e"},
		},
	}

	oldNums, newNums := hunkLineNumbers(hunk)

	assert.Equal(t, []int{8, 9, 0, 0, 10}, oldNums)
	assert.Equal(t, []int{20, 0, 21, 22, 23}, newNums)
}

func TestSliceSpans(t *testing.T) {
	spans := highlightedLine{
		{text: "foo", style: style.FgMagenta},
		{text: "Bar", style: style.FgBlue},
	}

	assert.Equal(t, highlightedLine{{text: "fo", style: style.FgMagenta}}, sliceSpans(spans, 0, 2))
	assert.Equal(t, highlightedLine{
		{text: "o", style: style.FgMagenta},
		{text: "B", style: style.FgBlue},
	}, sliceSpans(spans, 2, 4))
	assert.Equal(t, highlightedLine{{text: "ar", style: style.FgBlue}}, sliceSpans(spans, 4, 6))
	assert.Empty(t, sliceSpans(spans, 3, 3))
}

func TestFormatSplitGutterZeroWidth(t *testing.T) {
	assert.Equal(t, "", formatSplitGutter(CONTEXT, 5, 0))
}

func TestFormatSplitGutterStylesByKind(t *testing.T) {
	assert.Equal(t, style.FgRed.Sprint(" 9")+" ", formatSplitGutter(DELETION, 9, 3))
	assert.Equal(t, style.FgGreen.Sprint(" 9")+" ", formatSplitGutter(ADDITION, 9, 3))
	assert.Equal(t, style.FgBlackLighter.Sprint(" 9")+" ", formatSplitGutter(CONTEXT, 9, 3))
	assert.Equal(t, style.FgBlackLighter.Sprint("  ")+" ", formatSplitGutter(HUNK_HEADER, 9, 3))
}

// Regression test for a bug where context lines (and unpaired change lines)
// rendered as blank content in the split view: applyChangeEmphasis only
// built its safe fallback span when a change range was detected, so a nil
// `changed` (the normal case for context lines) fed straight-through a nil
// `spans` into sliceSpans, silently dropping the line's actual text.
func TestFormatSplitViewRendersContextLineContent(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n context line\n-old\n+new\n"

	patch := Parse(diff)
	result := utils.Decolorise(patch.FormatSplitView(FormatSplitViewOpts{Width: 40}))

	assert.Contains(t, result, "context line")
}

func TestFormatSplitViewNoChanges(t *testing.T) {
	patch := Parse(" context only\n")
	assert.Equal(t, "", patch.FormatSplitView(FormatSplitViewOpts{Width: 40}))
}

func TestFormatSplitViewAlignsModifiedLine(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n commit  string\n-date    string\n+date1   string\n version string\n"

	patch := Parse(diff)
	result := utils.Decolorise(patch.FormatSplitView(FormatSplitViewOpts{ShowLineNumbers: true, Width: 50}))
	lines := utils.SplitLines(result)

	// every body row (skipping the 3 header lines and the hunk-header line)
	// must be exactly Width runes wide, and the divider must land at the
	// same column on every row
	dividerCol := -1
	for _, line := range lines[4:] {
		assert.Len(t, []rune(line), 50)
		col := -1
		for i, r := range []rune(line) {
			if r == '│' {
				col = i
				break
			}
		}
		if dividerCol == -1 {
			dividerCol = col
		} else {
			assert.Equal(t, dividerCol, col, "divider must be in the same column on every row: %q", line)
		}
	}
}

func TestFormatSplitViewWrapsLongLineAndPadsShorterColumn(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-short\n+" +
		"this is a much much much longer replacement line that needs wrapping\n"

	patch := Parse(diff)
	result := utils.Decolorise(patch.FormatSplitView(FormatSplitViewOpts{ShowLineNumbers: true, Width: 50}))
	lines := utils.SplitLines(result)

	// header lines (3) + hunk header (1) + however many physical rows the
	// wrapped long line needs on the right - the short old line's column
	// must be padded with blank rows to match
	bodyRows := lines[4:]
	assert.Greater(t, len(bodyRows), 1, "the long replacement line should wrap to more than one row")

	for i, line := range bodyRows {
		assert.Len(t, []rune(line), 50, "row %d must still be padded to the full view width", i)
	}
}
