package patch_exploring

import (
	"testing"

	"github.com/jesseduffield/lazygit/pkg/gocui"
	"github.com/stretchr/testify/assert"
)

// Fixture used throughout this file. Global patch-line indices (header
// lines 0-2, hunk header 3, body from 4):
//
//	4  commit line
//	5  -old1        \
//	6  -old2         } one block: 3 deletions, 1 addition (excess-old)
//	7  -old3        /
//	8  +new1
//	9   ctx2
//	10 -modOld       } one block: 1 deletion, 1 addition (paired)
//	11 +modNew
//	12  ctx3
//
// Split rows (global indices), with row index alongside for reference:
//
//	0: {0,0}    header line 1
//	1: {1,1}    header line 2
//	2: {2,2}    header line 3
//	3: {3,3}    hunk header
//	4: {4,4}    commit line (context)
//	5: {5,8}    old1 / new1 (paired)
//	6: {6,-1}   old2 (excess)
//	7: {7,-1}   old3 (excess)
//	8: {9,9}    ctx2
//	9: {10,11}  modOld / modNew (paired)
//	10: {12,12} ctx3
const splitTestDiff = "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,7 +1,5 @@\n commit line\n-old1\n-old2\n-old3\n+new1\n ctx2\n-modOld\n+modNew\n ctx3\n"

func newSplitTestState(t *testing.T) *State {
	t.Helper()
	view := gocui.NewView("test", 0, 0, 79, 20, gocui.OutputNormal)
	view.Wrap = true
	state := NewState(splitTestDiff, "f", false, -1, view, nil, false, true)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	return state
}

func TestSplitStateInitialSelection(t *testing.T) {
	state := newSplitTestState(t)

	// the initial cursor should land on the first change (old1, row 5)
	assert.Equal(t, 5, state.selectedLineIdx)
	assert.Equal(t, 5, state.GetSelectedPatchLineIdx())
}

func TestSplitStateGetSelectedPatchLineIdxPerColumn(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(5) // old1/new1 paired row

	assert.Equal(t, 5, state.GetSelectedPatchLineIdx(), "old column should resolve to old1")

	state.SelectNewColumn()
	assert.Equal(t, 8, state.GetSelectedPatchLineIdx(), "new column should resolve to new1")

	state.SelectOldColumn()
	assert.Equal(t, 5, state.GetSelectedPatchLineIdx())
}

func TestSplitStateSelectColumnNoOpWhenBlank(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(6) // old2, excess row: new column is blank here

	state.SelectNewColumn()

	assert.Equal(t, 6, state.GetSelectedPatchLineIdx(), "switching to the blank column must be a no-op")
}

func TestSplitStateSelectColumnNoOpOutsideSplitMode(t *testing.T) {
	view := gocui.NewView("test", 0, 0, 79, 20, gocui.OutputNormal)
	view.Wrap = true
	state := NewState(simpleDiffForSplitTests, "f", false, -1, view, nil, false, false)
	if state == nil {
		t.Fatal("expected non-nil state")
	}

	state.SelectNewColumn()

	assert.False(t, state.splitMode)
}

const simpleDiffForSplitTests = "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-old\n+new\n"

func TestSplitStateFixSelectedColumnAutoFlipsOnNavigation(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(5)
	state.SelectNewColumn() // now on new1 (row 5, new column)
	assert.Equal(t, newColumn, state.selectedColumn)

	// moving down onto row 6 (old2 excess: new column blank) must
	// auto-flip the column to old, per fixSelectedColumn
	state.CycleLine(true)

	assert.Equal(t, 6, state.selectedLineIdx)
	assert.Equal(t, oldColumn, state.selectedColumn)
	assert.Equal(t, 6, state.GetSelectedPatchLineIdx())
}

func TestSplitStateCycleRangeStopsAtColumnBoundary(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(5)
	state.SelectNewColumn() // new1, row 5

	state.ToggleSelectRange(false)
	// growing the range forward would move onto row 6, where the new
	// column is blank - it must refuse rather than silently including
	// row 6 under the wrong column
	state.CycleRange(true)

	assert.Equal(t, 5, state.selectedLineIdx, "range must not extend onto a row blank for the locked column")
}

// HUNK selection spans a whole block of changes regardless of which column
// the cursor is on: for an asymmetric block (old1/old2/old3 vs new1) that
// means both the excess deletions AND the paired addition, since together
// they're one logical change - selecting only the deletions (as an earlier,
// column-locked implementation did) would let "stage this hunk" silently
// stage only half of a modified line. This holds regardless of which column
// the cursor started on.
func TestSplitStateSelectedPatchRangeForAsymmetricBlockOldLocked(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(5)
	state.ToggleSelectHunk() // selects the whole current block of changes

	firstIdx, lastIdx := state.SelectedPatchRange()

	// old1, old2, old3, new1 -> patch indices 5, 6, 7, 8
	assert.Equal(t, 5, firstIdx)
	assert.Equal(t, 8, lastIdx)
}

func TestSplitStateSelectedPatchRangeForAsymmetricBlockNewLocked(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(5)
	state.SelectNewColumn()
	state.ToggleSelectHunk()

	firstIdx, lastIdx := state.SelectedPatchRange()

	// starting column doesn't matter for HUNK mode - same whole-block range
	assert.Equal(t, 5, firstIdx)
	assert.Equal(t, 8, lastIdx)
}

func TestSplitStateLineIndicesOfAddedOrDeletedLinesInSelectedPatchRange(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(5)
	state.ToggleSelectHunk()

	// HUNK mode: both columns of the whole block, regardless of the
	// starting column (order follows the row walk: each row's old side then
	// its new side)
	assert.Equal(t, []int{5, 8, 6, 7}, state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange())

	// switching column exits HUNK mode (back to LINE), so this now reflects
	// just the current row's new-column line
	state.SelectNewColumn()
	assert.Equal(t, []int{8}, state.LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange())
}

func TestSplitStateSelectedPatchRangeForPairedBlock(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(9) // modOld/modNew paired row

	firstIdx, lastIdx := state.SelectedPatchRange()
	assert.Equal(t, 10, firstIdx)
	assert.Equal(t, 10, lastIdx)

	state.SelectNewColumn()
	firstIdx, lastIdx = state.SelectedPatchRange()
	assert.Equal(t, 11, firstIdx)
	assert.Equal(t, 11, lastIdx)
}

func TestSplitStateCurrentHunkBounds(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(9)

	start, end := state.CurrentHunkBounds()

	// the whole hunk: header (idx 3) through the last body line (idx 12)
	assert.Equal(t, 3, start)
	assert.Equal(t, 12, end)
}

func TestSplitStateRenderForLineIndicesRefreshesRowsAndClamps(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(10) // last row

	rendered := state.RenderForLineIndices([]int{5})
	assert.NotEmpty(t, rendered)
	assert.Equal(t, 11, len(state.splitRows))
	assert.Equal(t, 10, state.selectedLineIdx, "selection within bounds must be left alone")
}

func TestSplitStateSelectPreviousAndNextHunk(t *testing.T) {
	// SelectNextHunk/SelectPreviousHunk jump to the next/previous *block of
	// changes*, which may be within the same enclosing @@ hunk (as here,
	// where the two blocks are only separated by a context line) or in a
	// genuinely different one - both are exercised below.
	diff := splitTestDiff + "@@ -20,1 +18,1 @@\n-veryold\n+verynew\n"
	view := gocui.NewView("test", 0, 0, 79, 20, gocui.OutputNormal)
	view.Wrap = true
	state := NewState(diff, "f", false, -1, view, nil, false, true)
	if state == nil {
		t.Fatal("expected non-nil state")
	}

	state.SelectLine(5) // first block (old1/new1), patch idx 5

	state.SelectNextHunk()
	assert.Equal(t, 10, state.GetSelectedPatchLineIdx(), "should have moved to the modOld/modNew block")

	state.SelectNextHunk()
	assert.Equal(t, 14, state.GetSelectedPatchLineIdx(), "should have moved into the second @@ hunk")

	state.SelectPreviousHunk()
	assert.Equal(t, 10, state.GetSelectedPatchLineIdx())

	state.SelectPreviousHunk()
	assert.Equal(t, 5, state.GetSelectedPatchLineIdx())
}

func TestSplitStateOnViewWidthChangedPreservesCursorTarget(t *testing.T) {
	state := newSplitTestState(t)
	state.SelectLine(9) // modOld/modNew paired row
	state.SelectNewColumn()
	assert.Equal(t, 11, state.GetSelectedPatchLineIdx())

	narrower := gocui.NewView("test", 0, 0, 39, 20, gocui.OutputNormal)
	narrower.Wrap = true
	state.OnViewWidthChanged(narrower)

	// the cursor should still be resolving to the same patch line (modNew)
	// after the resize, even though the row layout was recomputed
	assert.Equal(t, 11, state.GetSelectedPatchLineIdx())
}

func TestSplitStateNewStatePreservesSelectionAcrossModeSwitch(t *testing.T) {
	unifiedView := gocui.NewView("test", 0, 0, 79, 20, gocui.OutputNormal)
	unifiedView.Wrap = true
	unifiedState := NewState(splitTestDiff, "f", false, -1, unifiedView, nil, false, false)
	if unifiedState == nil {
		t.Fatal("expected non-nil state")
	}
	unifiedState.SelectLine(unifiedState.rowForPatchIdx(11)) // modNew

	splitView := gocui.NewView("test", 0, 0, 79, 20, gocui.OutputNormal)
	splitView.Wrap = true
	// diff unchanged, only splitMode flips - NewState must not just return
	// oldState as-is (its diff==oldState.diff short-circuit is gated on
	// splitMode matching too), and must carry the selection across via
	// patch-line index, not raw row index
	splitState := NewState(splitTestDiff, "f", false, -1, splitView, unifiedState, false, true)
	if splitState == nil {
		t.Fatal("expected non-nil state")
	}

	assert.True(t, splitState.splitMode)
	assert.Equal(t, 11, splitState.GetSelectedPatchLineIdx())
}
