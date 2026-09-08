package patch

import (
	"strings"
	"testing"

	"github.com/jesseduffield/generics/set"
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
	assert.Equal(t, "", formatSplitGutter(CONTEXT, 5, 0, false))
}

func TestFormatSplitGutterStylesByKind(t *testing.T) {
	assert.Equal(t, style.FgRed.MergeStyle(splitViewDeletionBg).Sprint(" 9 "), formatSplitGutter(DELETION, 9, 3, false))
	assert.Equal(t, style.FgGreen.MergeStyle(splitViewAdditionBg).Sprint(" 9 "), formatSplitGutter(ADDITION, 9, 3, false))
	assert.Equal(t, style.FgBlackLighter.Sprint(" 9 "), formatSplitGutter(CONTEXT, 9, 3, false))
	assert.Equal(t, style.FgBlackLighter.Sprint("   "), formatSplitGutter(HUNK_HEADER, 9, 3, false))
}

// Staging a line (included) overrides the row tint above with the brighter
// BgGreen "included" indicator, rather than stacking both.
func TestFormatSplitGutterIncludedAddsBackground(t *testing.T) {
	assert.Equal(t, style.FgRed.MergeStyle(style.BgGreen).Sprint(" 9 "), formatSplitGutter(DELETION, 9, 3, true))
}

// Regression test for a real color bug: TextStyle promotes an entire style
// to 24-bit RGB the moment either its fg or bg is RGB (see
// TextStyle.deriveStyle), so pairing an RGB row background with the basic
// FgRed/FgGreen patchLineStyle already uses would silently reinterpret that
// basic color through gookit's own fixed RGB approximation instead of the
// terminal's actual theme color. splitViewAdditionBg/splitViewDeletionBg use
// an 8-bit (256-color) background instead, which TextStyle.deriveStyle keeps
// separate from the RGB-promotion path (see derive256Style) - verify the fg
// code in the rendered output is still the plain basic ANSI code (31/32),
// not a 24-bit "38;2;..." sequence.
func TestSplitViewRowBgKeepsBasicForegroundIntact(t *testing.T) {
	rendered := style.FgRed.MergeStyle(splitViewDeletionBg).Sprint("x")
	assert.Contains(t, rendered, "\x1b[31;")
	assert.NotContains(t, rendered, "38;2;")

	rendered = style.FgGreen.MergeStyle(splitViewAdditionBg).Sprint("x")
	assert.Contains(t, rendered, "\x1b[32;")
	assert.NotContains(t, rendered, "38;2;")
}

// Regression test for a bug where context lines (and unpaired change lines)
// rendered as blank content in the split view: applyChangeEmphasis only
// built its safe fallback span when a change range was detected, so a nil
// `changed` (the normal case for context lines) fed straight-through a nil
// `spans` into sliceSpans, silently dropping the line's actual text.
func TestFormatSplitViewRendersContextLineContent(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n context line\n-old\n+new\n"

	patch := Parse(diff)
	rendered, _ := patch.FormatSplitView(FormatSplitViewOpts{Width: 40})
	result := utils.Decolorise(rendered)

	assert.Contains(t, result, "context line")
}

// Regression test for a real crash: WrapViewLinesToWidth expands tabs to
// spaces internally before wrapping, so a wrapped chunk of a tab-containing
// line is not a literal substring of the unexpanded line - reconstructing
// its byte offset via a naive strings.Index into the original line could
// return -1, producing an inverted (start > end) slice and panicking.
// Regression test: expandTabsWithOffsetMap must expand each tab relative to
// the already-expanded output's own column, not to the original string's
// byte index - otherwise a second (or later) tab on the same line, which
// starts at a byte index lower than its true visual column, expands short.
func TestExpandTabsWithOffsetMapConsecutiveTabs(t *testing.T) {
	expanded, offsets := expandTabsWithOffsetMap("\t\tx", 4)

	// two tabs from column 0 land on true 4-column stops: 4 spaces, then 4
	// more (not 3, which is what indexing by original byte index 1 would
	// give for the second tab)
	assert.Equal(t, "        x", expanded)
	assert.Len(t, offsets, len(expanded)+1)
}

func TestFormatSplitViewWrapsTabContainingLineWithoutPanicking(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-old\n+a\ta\ta\ta\ta\ta\ta\ta\ta\ta\ta\ta\ta\ta\t very long line many tabs wrap across rows for sure yes\n"

	patch := Parse(diff)

	assert.NotPanics(t, func() {
		rendered, rows := patch.FormatSplitView(FormatSplitViewOpts{Width: 30})
		assert.NotEmpty(t, rendered)
		assert.NotEmpty(t, rows)
	})
}

func TestFormatSplitViewNoChanges(t *testing.T) {
	patch := Parse(" context only\n")
	rendered, rows := patch.FormatSplitView(FormatSplitViewOpts{Width: 40})
	assert.Equal(t, "", rendered)
	assert.Nil(t, rows)
}

func TestFormatSplitViewAlignsModifiedLine(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n commit  string\n-date    string\n+date1   string\n version string\n"

	patch := Parse(diff)
	rendered, rows := patch.FormatSplitView(FormatSplitViewOpts{ShowLineNumbers: true, Width: 50})
	result := utils.Decolorise(rendered)
	lines := utils.SplitLines(result)

	// one SplitRow per physical line, and the row list must never disagree
	// with the actual rendered line count - this is the exact invariant the
	// interactive cursor depends on
	assert.Len(t, rows, len(lines))

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

	// global patch-line indices: 3 header lines (0-2), hunk header (3),
	// then the 3 body rows (4-6) - the modified pair shares row 5, with
	// distinct old/new indices
	assert.Equal(t, []SplitRow{
		{Old: 0, New: 0},
		{Old: 1, New: 1},
		{Old: 2, New: 2},
		{Old: 3, New: 3},
		{Old: 4, New: 4},
		{Old: 5, New: 6},
		{Old: 7, New: 7},
	}, rows)
}

// visualColumnOf returns the on-screen column of the first occurrence of
// target in a decolorised, tab-unexpanded line, simulating real terminal tab
// expansion (4-column stops from column 0) - unlike a plain rune index, this
// matches what a viewer actually sees, which is what a divider-alignment
// check needs when rows contain differing numbers of tabs.
func visualColumnOf(line string, target rune) int {
	col := 0
	for _, r := range line {
		if r == target {
			return col
		}
		if r == '\t' {
			col += 4 - (col % 4)
		} else {
			col++
		}
	}
	return -1
}

// Regression test for a real bug: two rows with different indentation levels
// (so different tab counts) rendered their dividers at different visual
// columns, because expandTabsWithOffsetMap under-counted every tab after the
// first one on a line (see TestExpandTabsWithOffsetMapConsecutiveTabs).
func TestFormatSplitViewAlignsColumnsAcrossDifferingTabCounts(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-\t\ttwo tabs\n+\t\ttwo tabs!\n \tone tab\n"

	patch := Parse(diff)
	rendered, _ := patch.FormatSplitView(FormatSplitViewOpts{ShowLineNumbers: true, Width: 60})
	lines := utils.SplitLines(utils.Decolorise(rendered))

	dividerCol := -1
	for _, line := range lines[4:] {
		col := visualColumnOf(line, '│')
		if dividerCol == -1 {
			dividerCol = col
		} else {
			assert.Equal(t, dividerCol, col, "divider must land on the same visual column regardless of a row's tab count: %q", line)
		}
	}
}

// The global patch-line indices FormatSplitView emits must agree with
// Patch.Lines()'s own indexing (that's what staging/selection is keyed on),
// not just be internally self-consistent. Verify by cross-checking the
// content at each SplitRow's indices against Patch.Lines() directly.
func TestFormatSplitViewGlobalIndicesMatchPatchLines(t *testing.T) {
	patch := Parse(twoHunks)
	_, rows := patch.FormatSplitView(FormatSplitViewOpts{Width: 60})

	lines := patch.Lines()
	for _, row := range rows {
		if row.Old >= 0 {
			assert.Less(t, row.Old, len(lines))
		}
		if row.New >= 0 {
			assert.Less(t, row.New, len(lines))
		}
	}

	// spot-check: the second hunk's header ("@@ -8,6 +8,8 @@ grape") is a
	// mirrored (old==new) row, and its index must land on the actual
	// HUNK_HEADER line in Patch.Lines()
	secondHunkHeaderIdx := patch.HunkStartIdx(1)
	assert.Contains(t, rows, SplitRow{Old: secondHunkHeaderIdx, New: secondHunkHeaderIdx})
	assert.Equal(t, HUNK_HEADER, lines[secondHunkHeaderIdx].Kind)
}

func TestFormatSplitViewWrapsLongLineAndPadsShorterColumn(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n-short\n+" +
		"this is a much much much longer replacement line that needs wrapping\n"

	patch := Parse(diff)
	rendered, rows := patch.FormatSplitView(FormatSplitViewOpts{ShowLineNumbers: true, Width: 50})
	result := utils.Decolorise(rendered)
	lines := utils.SplitLines(result)

	assert.Len(t, rows, len(lines))

	// header lines (3) + hunk header (1) + however many physical rows the
	// wrapped long line needs on the right - the short old line's column
	// must be padded with blank rows to match
	bodyRows := lines[4:]
	assert.Greater(t, len(bodyRows), 1, "the long replacement line should wrap to more than one row")

	for i, line := range bodyRows {
		assert.Len(t, []rune(line), 50, "row %d must still be padded to the full view width", i)
	}

	// every wrapped continuation row must repeat the same (old, new) patch
	// indices as the row it continues, exactly like the unified view's
	// wrapping maps several view lines back to one patch line
	bodySplitRows := rows[4:]
	for i := 1; i < len(bodySplitRows); i++ {
		assert.Equal(t, bodySplitRows[0], bodySplitRows[i],
			"continuation row %d must repeat the same patch-line indices", i)
	}
}

func TestFormatSplitViewIncludedLinesAreIndependentPerColumn(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n commit  string\n-date    string\n+date1   string\n version string\n"

	patch := Parse(diff)
	// the modified pair is at global indices 5 (deletion) and 6 (addition);
	// only mark the deletion as included
	rendered, _ := patch.FormatSplitView(FormatSplitViewOpts{Width: 40, IncLineIndices: set.NewFromSlice([]int{5})})

	lines := strings.Split(rendered, "\n")
	modifiedLine := lines[5]
	halves := strings.SplitN(modifiedLine, splitDivider, 2)
	if !assert.Len(t, halves, 2) {
		return
	}
	includedPrefix := strings.SplitN(style.FgRed.MergeStyle(style.BgGreen).Sprint("x"), "x", 2)[0]
	assert.Contains(t, halves[0], includedPrefix, "the old (included) side must have a green background")
	assert.NotContains(t, halves[1], includedPrefix, "the new (not included) side must not")
}
