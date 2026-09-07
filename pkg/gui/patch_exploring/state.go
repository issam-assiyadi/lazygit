package patch_exploring

import (
	"strings"

	"github.com/jesseduffield/generics/set"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"
	"github.com/jesseduffield/lazygit/pkg/gocui"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/samber/lo"
)

// splitColumn identifies which side of a split-view row the cursor is on.
// Only meaningful when State.splitMode is true.
type splitColumn int

const (
	oldColumn splitColumn = iota
	newColumn
)

// State represents the current state of the patch explorer context i.e. when
// you're staging a file or you're building a patch from an existing commit
// this struct holds the info about the diff you're interacting with and what's currently selected.
type State struct {
	// These are in terms of view lines (wrapped) in unified mode, or
	// physical split-view rows (see splitRows) in split mode - either way,
	// a display row the cursor can land on.
	selectedLineIdx   int
	rangeStartLineIdx int
	// If a range is sticky, it means we expand the range when we move up or down.
	// Otherwise, we cancel the range when we move up or down.
	rangeIsSticky bool
	diff          string
	patch         *patch.Patch
	selectMode    selectMode

	// whether body lines are prefixed with an old/new line-number gutter;
	// kept around so we can recompute the wrap width when the view is resized
	showLineNumbers bool
	// the width, in characters, of the line-number gutter (0 if
	// showLineNumbers is false, or if the patch has no hunks). Unified mode
	// only.
	gutterWidth int
	// the view's current width, used to pad hunk header lines' background
	// out to the full width of the view
	viewWidth int

	// Array of indices of the wrapped lines indexed by a patch line index.
	// Unified mode only.
	viewLineIndices []int
	// Array of indices of the original patch lines indexed by a wrapped view line index.
	// Unified mode only.
	patchLineIndices []int

	// if true, render and navigate this patch as two side-by-side columns
	// (old content left, new content right) rather than a single unified
	// column. The fields below are only meaningful in this mode.
	splitMode bool
	// which column the cursor is currently on
	selectedColumn splitColumn
	// the physical-row mapping for the current split-view rendering, as
	// emitted by Patch.FormatSplitView - the single source of truth for how
	// many rows there are and which patch-line index each column of each
	// row holds. Refreshed on every render (see RenderForLineIndices) and
	// on resize (see OnViewWidthChanged), since it depends on the view's
	// current width just like unified mode's wrap bookkeeping does.
	splitRows []patch.SplitRow
	// reverse index from a patch-line index to the physical row that holds
	// it (on either column), rebuilt alongside splitRows.
	splitPatchIdxToRow map[int]int

	// whether the user has switched to hunk mode manually; if hunk mode is on
	// but this is false, then hunk mode was enabled because the config makes it
	// on by default.
	// this makes a difference for whether we want to escape out of hunk mode
	userEnabledHunkMode bool
}

// these represent what select mode we're in
type selectMode int

const (
	LINE selectMode = iota
	RANGE
	HUNK
)

func NewState(diff string, filename string, showLineNumbers bool, selectedLineIdx int, view *gocui.View, oldState *State, useHunkModeByDefault bool, splitMode bool) *State {
	if oldState != nil && diff == oldState.diff && selectedLineIdx == -1 && splitMode == oldState.splitMode {
		// if we're here then we can return the old state. If selectedLineIdx was not -1
		// then that would mean we were trying to click and potentially drag a range, which
		// is why in that case we continue below
		return oldState
	}

	p := patch.Parse(diff).SetFilename(filename)

	if !p.ContainsChanges() {
		return nil
	}

	rangeStartLineIdx := 0
	if oldState != nil {
		rangeStartLineIdx = oldState.rangeStartLineIdx
	}

	selectMode := LINE
	if useHunkModeByDefault && !p.IsSingleHunkForWholeFile() {
		selectMode = HUNK
	}

	userEnabledHunkMode := false
	if oldState != nil {
		userEnabledHunkMode = oldState.userEnabledHunkMode
	}

	state := &State{
		patch:               p,
		selectMode:          selectMode,
		rangeStartLineIdx:   rangeStartLineIdx,
		rangeIsSticky:       false,
		diff:                diff,
		showLineNumbers:     showLineNumbers,
		viewWidth:           view.InnerWidth(),
		splitMode:           splitMode,
		selectedColumn:      oldColumn,
		userEnabledHunkMode: userEnabledHunkMode,
	}

	if splitMode {
		state.refreshSplitRows()
	} else {
		state.gutterWidth = p.GutterWidth(showLineNumbers)
		state.viewLineIndices, state.patchLineIndices = wrapPatchLines(p, state.gutterWidth, view)
	}

	rowCount := state.rowCount()

	// if we have clicked from the outside to focus the main view we'll pass in a non-negative line index so that we can instantly select that line
	if selectedLineIdx >= 0 {
		// Clamp to the number of rows; index might be out of bounds if a
		// custom diff renderer is being used which produces more lines
		selectedLineIdx = min(selectedLineIdx, rowCount-1)

		state.selectMode = RANGE
		state.rangeStartLineIdx = selectedLineIdx
		state.selectedLineIdx = selectedLineIdx
	} else if oldState != nil {
		// if we previously had a selectMode of RANGE, we want that to now be line again (or hunk, if that's the default)
		if oldState.selectMode != RANGE {
			state.selectMode = oldState.selectMode
		}
		oldPatchLineIdx := oldState.GetSelectedPatchLineIdx()
		newPatchLineIdx := p.GetNextChangeIdx(oldPatchLineIdx)
		// When staging an addition from a consecutive changes block, the unselected deletions get
		// reordered to appear before the remaining additions in the new diff. This can cause the
		// cursor to land on a deletion at the same patch line index where the staged addition used
		// to be. In that case, skip forward past any deletions, then call GetNextChangeIdx from the
		// first non-deletion position, which correctly lands on the next meaningful change.
		newLines := p.Lines()
		if newPatchLineIdx == oldPatchLineIdx &&
			oldState.patch.Lines()[oldPatchLineIdx].IsAddition() &&
			newLines[newPatchLineIdx].IsDeletion() &&
			p.HunkOldStartForLine(newPatchLineIdx) == oldState.patch.HunkOldStartForLine(oldPatchLineIdx) {
			for newPatchLineIdx < len(newLines) && newLines[newPatchLineIdx].IsDeletion() {
				newPatchLineIdx++
			}
			newPatchLineIdx = p.GetNextChangeIdx(newPatchLineIdx)
		}
		state.selectRowAndColumnForPatchIdx(newPatchLineIdx)
	} else {
		state.selectRowAndColumnForPatchIdx(p.GetNextChangeIdx(0))
	}

	state.fixSelectedColumn()

	return state
}

// rowCount returns the number of display rows the cursor can move over,
// regardless of mode.
func (s *State) rowCount() int {
	if s.splitMode {
		return len(s.splitRows)
	}
	return len(s.patchLineIndices)
}

// refreshSplitRows re-renders the patch as a split view purely to obtain a
// fresh physical-row mapping for the view's current width (the string
// itself is discarded here; RenderForLineIndices is what actually renders
// for display, and refreshes this same mapping again from that same call,
// so there's only ever one place that computes "how many rows, holding
// which patch lines" for a given width).
func (s *State) refreshSplitRows() {
	_, rows := s.patch.FormatSplitView(patch.FormatSplitViewOpts{
		ShowLineNumbers: s.showLineNumbers,
		Width:           s.viewWidth,
	})
	s.splitRows = rows
	s.rebuildSplitPatchIdxToRow()
}

func (s *State) rebuildSplitPatchIdxToRow() {
	s.splitPatchIdxToRow = make(map[int]int, len(s.splitRows))
	for rowIdx, row := range s.splitRows {
		if row.Old >= 0 {
			s.splitPatchIdxToRow[row.Old] = rowIdx
		}
		if row.New >= 0 {
			s.splitPatchIdxToRow[row.New] = rowIdx
		}
	}
}

// fixSelectedColumn ensures selectedColumn points at a populated cell for
// the current row, falling back to whichever side actually has content. A
// no-op outside split mode. This is the single enforcement point for the
// invariant that GetSelectedPatchLineIdx must never need to guess: every
// navigation path that changes selectedLineIdx calls this immediately
// after (either via selectLineWithoutRangeCheck, or explicitly for the few
// paths that assign selectedLineIdx directly).
func (s *State) fixSelectedColumn() {
	if !s.splitMode || len(s.splitRows) == 0 {
		return
	}
	row := s.splitRows[s.selectedLineIdx]
	if s.selectedColumn == oldColumn && row.Old < 0 {
		s.selectedColumn = newColumn
	} else if s.selectedColumn == newColumn && row.New < 0 {
		s.selectedColumn = oldColumn
	}
}

// InSplitMode reports whether this state is rendering/navigating the patch
// as a side-by-side split view.
func (s *State) InSplitMode() bool {
	return s.splitMode
}

// SelectOldColumn moves the cursor to the old (left) column of the current
// row. SelectNewColumn is its new (right) column counterpart. Both are a
// no-op outside split mode, if already on the requested column, or if it's
// blank on the current row. Switching columns while range- or
// hunk-selecting exits that selection mode first, since a range/hunk
// selection is locked to a single column for its whole extent (see
// SelectedPatchRange) - switching column mid-selection would leave
// already-selected rows ambiguous.
func (s *State) SelectOldColumn() {
	s.selectColumn(oldColumn)
}

func (s *State) SelectNewColumn() {
	s.selectColumn(newColumn)
}

func (s *State) selectColumn(column splitColumn) {
	if !s.splitMode || len(s.splitRows) == 0 || s.selectedColumn == column {
		return
	}
	if !s.rowHasColumn(s.selectedLineIdx, column) {
		return
	}

	s.selectMode = LINE
	s.selectedColumn = column
}

func (s *State) rowHasColumn(rowIdx int, column splitColumn) bool {
	row := s.splitRows[rowIdx]
	if column == oldColumn {
		return row.Old >= 0
	}
	return row.New >= 0
}

// rowForPatchIdx converts a patch-line index (Patch.Lines() indexing) to
// the display row that holds it - the inverse of patchLineIdxAtRow/
// strictPatchLineIdxAtRow.
func (s *State) rowForPatchIdx(patchIdx int) int {
	if !s.splitMode {
		return s.viewLineIndices[patchIdx]
	}
	if rowIdx, ok := s.splitPatchIdxToRow[patchIdx]; ok {
		return rowIdx
	}
	return 0
}

// selectRowAndColumnForPatchIdx moves the cursor to the row holding
// patchIdx, and (in split mode) sets selectedColumn to whichever side of
// that row patchIdx is actually on - rowForPatchIdx alone would leave
// selectedColumn at its previous value, which is wrong whenever the target
// patch line only exists on the other side (e.g. an addition when the
// cursor was previously on the old column).
func (s *State) selectRowAndColumnForPatchIdx(patchIdx int) {
	s.selectedLineIdx = s.rowForPatchIdx(patchIdx)
	s.setColumnForPatchIdx(patchIdx)
}

// setColumnForPatchIdx sets selectedColumn to whichever side of the
// *current* selectedLineIdx holds patchIdx. A no-op outside split mode.
func (s *State) setColumnForPatchIdx(patchIdx int) {
	if !s.splitMode {
		return
	}
	if s.splitRows[s.selectedLineIdx].New == patchIdx {
		s.selectedColumn = newColumn
	} else {
		s.selectedColumn = oldColumn
	}
}

// strictPatchLineIdxAtRow returns the patch-line index held by the given
// row's given column, or -1 if that column is blank on that row (which is
// only possible in split mode). Used where "blank" must mean "skip this
// row", as opposed to patchLineIdxAtRow's fallback behaviour.
func (s *State) strictPatchLineIdxAtRow(rowIdx int, column splitColumn) int {
	if !s.splitMode {
		return s.patchLineIndices[rowIdx]
	}
	row := s.splitRows[rowIdx]
	if column == oldColumn {
		return row.Old
	}
	return row.New
}

// patchLineIdxAtRow returns the patch-line index held by the given row's
// given column, falling back to the other column if the requested one is
// blank. This is safe to use for the *currently selected* row specifically,
// because fixSelectedColumn guarantees the selected row's selected column
// is always populated; the fallback here is only a defensive last resort.
func (s *State) patchLineIdxAtRow(rowIdx int, column splitColumn) int {
	if idx := s.strictPatchLineIdxAtRow(rowIdx, column); idx >= 0 {
		return idx
	}
	other := newColumn
	if column == newColumn {
		other = oldColumn
	}
	return s.strictPatchLineIdxAtRow(rowIdx, other)
}

// nearestPatchLineIdxInColumn resolves the patch-line index at row `from`
// for the given column, searching towards `towards` if `from`'s cell is
// blank on that column. This can happen at the edge of a HUNK-mode
// selection (which spans a whole block of changes regardless of column)
// when the block has excess lines on the *other* side from the locked
// column - those excess rows are blank on this column and must be excluded
// from the range, not substituted with the other column's line.
func (s *State) nearestPatchLineIdxInColumn(from int, towards int, column splitColumn) int {
	step := 1
	if towards < from {
		step = -1
	}
	for row := from; ; row += step {
		if idx := s.strictPatchLineIdxAtRow(row, column); idx >= 0 {
			return idx
		}
		if row == towards {
			break
		}
	}
	// every row from `from` to `towards` is blank on this column - shouldn't
	// happen (a selection always contains at least the originally-selected
	// row, which fixSelectedColumn guarantees is populated on the selected
	// column), but fall back to the unlocked resolution rather than a
	// sentinel.
	return s.patchLineIdxAtRow(from, column)
}

func (s *State) OnViewWidthChanged(view *gocui.View) {
	s.viewWidth = view.InnerWidth()

	if !view.Wrap {
		return
	}

	if s.splitMode {
		// Unlike unified mode's wrapPatchLines, FormatSplitView always
		// wraps its columns internally regardless of view.Wrap, so a fresh
		// render (not a separate recomputation) is the only way to get a
		// row mapping that matches the new width - see refreshSplitRows.
		selectedPatchLineIdx := s.GetSelectedPatchLineIdx()
		s.refreshSplitRows()
		s.selectedLineIdx = s.clampLineIdx(s.rowForPatchIdx(selectedPatchLineIdx))
		s.setColumnForPatchIdx(selectedPatchLineIdx)
		if s.selectMode == RANGE {
			rangeStartPatchLineIdx := s.patchLineIdxAtRow(s.rangeStartLineIdx, s.selectedColumn)
			s.rangeStartLineIdx = s.clampLineIdx(s.rowForPatchIdx(rangeStartPatchLineIdx))
		}
		s.fixSelectedColumn()
		return
	}

	selectedPatchLineIdx := s.patchLineIndices[s.selectedLineIdx]
	var rangeStartPatchLineIdx int
	if s.selectMode == RANGE {
		rangeStartPatchLineIdx = s.patchLineIndices[s.rangeStartLineIdx]
	}
	s.viewLineIndices, s.patchLineIndices = wrapPatchLines(s.patch, s.gutterWidth, view)
	s.selectedLineIdx = s.viewLineIndices[selectedPatchLineIdx]
	if s.selectMode == RANGE {
		s.rangeStartLineIdx = s.viewLineIndices[rangeStartPatchLineIdx]
	}
}

func (s *State) GetSelectedPatchLineIdx() int {
	return s.patchLineIdxAtRow(s.selectedLineIdx, s.selectedColumn)
}

func (s *State) GetSelectedViewLineIdx() int {
	return s.selectedLineIdx
}

func (s *State) GetDiff() string {
	return s.diff
}

func (s *State) ToggleSelectHunk() {
	if s.selectMode == HUNK {
		s.selectMode = LINE
	} else {
		s.selectMode = HUNK
		s.userEnabledHunkMode = true

		// If we are not currently on a change line, select the next one (or the
		// previous one if there is no next one):
		s.selectRowAndColumnForPatchIdx(s.patch.GetNextChangeIdx(s.GetSelectedPatchLineIdx()))
	}
}

func (s *State) ToggleStickySelectRange() {
	s.ToggleSelectRange(true)
}

func (s *State) ToggleSelectRange(sticky bool) {
	if s.SelectingRange() {
		s.selectMode = LINE
	} else {
		s.selectMode = RANGE
		s.rangeStartLineIdx = s.selectedLineIdx
		s.rangeIsSticky = sticky
	}
}

func (s *State) SetRangeIsSticky(value bool) {
	s.rangeIsSticky = value
}

func (s *State) SelectingHunk() bool {
	return s.selectMode == HUNK
}

func (s *State) SelectingHunkEnabledByUser() bool {
	return s.selectMode == HUNK && s.userEnabledHunkMode
}

func (s *State) SelectingRange() bool {
	return s.selectMode == RANGE && (s.rangeIsSticky || s.rangeStartLineIdx != s.selectedLineIdx)
}

func (s *State) SelectingLine() bool {
	return s.selectMode == LINE
}

func (s *State) SetLineSelectMode() {
	s.selectMode = LINE
}

func (s *State) DismissHunkSelectMode() {
	if s.SelectingHunk() {
		s.selectMode = LINE
	}
}

// For when you move the cursor without holding shift (meaning if we're in
// a non-sticky range select, we'll cancel it)
func (s *State) SelectLine(newSelectedLineIdx int) {
	if s.selectMode == RANGE && !s.rangeIsSticky {
		s.selectMode = LINE
	}

	s.selectLineWithoutRangeCheck(newSelectedLineIdx)
}

func (s *State) clampLineIdx(lineIdx int) int {
	return lo.Clamp(lineIdx, 0, s.rowCount()-1)
}

// This just moves the cursor without caring about range select
func (s *State) selectLineWithoutRangeCheck(newSelectedLineIdx int) {
	s.selectedLineIdx = s.clampLineIdx(newSelectedLineIdx)
	s.fixSelectedColumn()
}

func (s *State) SelectNewLineForRange(newSelectedLineIdx int) {
	s.rangeStartLineIdx = s.clampLineIdx(newSelectedLineIdx)

	s.selectMode = RANGE

	s.selectLineWithoutRangeCheck(newSelectedLineIdx)
}

func (s *State) DragSelectLine(newSelectedLineIdx int) {
	s.selectMode = RANGE

	s.selectLineWithoutRangeCheck(newSelectedLineIdx)
}

func (s *State) CycleSelection(forward bool) {
	if s.SelectingHunk() {
		if forward {
			s.SelectNextHunk()
		} else {
			s.SelectPreviousHunk()
		}
	} else {
		s.CycleLine(forward)
	}
}

func (s *State) SelectPreviousHunk() {
	patchLines := s.patch.Lines()
	patchLineIdx := s.GetSelectedPatchLineIdx()
	nextNonChangeLine := patchLineIdx
	for nextNonChangeLine >= 0 && patchLines[nextNonChangeLine].IsChange() {
		nextNonChangeLine--
	}
	nextChangeLine := nextNonChangeLine
	for nextChangeLine >= 0 && !patchLines[nextChangeLine].IsChange() {
		nextChangeLine--
	}
	if nextChangeLine >= 0 {
		// Now we found a previous hunk, but we're on its last line. Skip to the beginning.
		for nextChangeLine > 0 && patchLines[nextChangeLine-1].IsChange() {
			nextChangeLine--
		}
		s.selectRowAndColumnForPatchIdx(nextChangeLine)
	}
}

func (s *State) SelectNextHunk() {
	patchLines := s.patch.Lines()
	patchLineIdx := s.GetSelectedPatchLineIdx()
	nextNonChangeLine := patchLineIdx
	for nextNonChangeLine < len(patchLines) && patchLines[nextNonChangeLine].IsChange() {
		nextNonChangeLine++
	}
	nextChangeLine := nextNonChangeLine
	for nextChangeLine < len(patchLines) && !patchLines[nextChangeLine].IsChange() {
		nextChangeLine++
	}
	if nextChangeLine < len(patchLines) {
		s.selectRowAndColumnForPatchIdx(nextChangeLine)
	}
}

func (s *State) CycleLine(forward bool) {
	change := 1
	if !forward {
		change = -1
	}

	s.SelectLine(s.selectedLineIdx + change)
}

// This is called when we use shift+arrow to expand the range (i.e. a non-sticky
// range)
func (s *State) CycleRange(forward bool) {
	if !s.SelectingRange() {
		s.ToggleSelectRange(false)
	}

	s.SetRangeIsSticky(false)

	change := 1
	if !forward {
		change = -1
	}

	next := s.selectedLineIdx + change
	if s.splitMode && next >= 0 && next < len(s.splitRows) && !s.rowHasColumn(next, s.selectedColumn) {
		// Don't extend a range across a row where the active column is
		// blank: range-select is column-locked (see SelectedPatchRange), so
		// crossing into a row with nothing on this side would either
		// dead-end or silently start referring to the other column's
		// lines. Simplest correct behaviour: stop at the boundary.
		return
	}

	s.selectLineWithoutRangeCheck(next)
}

// returns first and last patch line index of current hunk
func (s *State) CurrentHunkBounds() (int, int) {
	hunkIdx := s.patch.HunkContainingLine(s.GetSelectedPatchLineIdx())
	start := s.patch.HunkStartIdx(hunkIdx)
	end := s.patch.HunkEndIdx(hunkIdx)
	return start, end
}

// selectionRangeForCurrentBlockOfChanges finds the contiguous run of rows
// around the current selection where at least one column holds a change
// line (addition/deletion), by walking display rows directly rather than
// flat patch-line indices.
//
// This has to walk rows, not patch lines: in split mode, row order isn't a
// monotonic function of patch-line index. A block with more deletions than
// additions pairs the first deletion with the (numerically much higher)
// first addition into one row, then gives each *excess* deletion its own
// later row - so a deletion whose patch-line index sits between the paired
// deletion and the paired addition can still end up in a *later* row than
// the addition. Converting the flat block's two patch-index endpoints
// independently (as unified mode's single-column layout allows) would
// therefore miss rows in the middle. Walking rows sidesteps this, and
// happens to also naturally include wrapped continuation rows (which
// repeat their parent row's indices, so rowIsChange agrees) without a
// separate extension step - so this same implementation replaces the
// unified-only version this used to be.
func (s *State) selectionRangeForCurrentBlockOfChanges() (int, int) {
	viewStart := s.selectedLineIdx
	for viewStart > 0 && s.rowIsChange(viewStart-1) {
		viewStart--
	}

	viewEnd := s.selectedLineIdx
	for viewEnd < s.rowCount()-1 && s.rowIsChange(viewEnd+1) {
		viewEnd++
	}

	return viewStart, viewEnd
}

// rowIsChange reports whether the given display row holds a change line
// (addition/deletion) on at least one column.
func (s *State) rowIsChange(rowIdx int) bool {
	lines := s.patch.Lines()

	if !s.splitMode {
		return lines[s.patchLineIndices[rowIdx]].IsChange()
	}

	row := s.splitRows[rowIdx]
	return (row.Old >= 0 && lines[row.Old].IsChange()) || (row.New >= 0 && lines[row.New].IsChange())
}

func (s *State) SelectedViewRange() (int, int) {
	switch s.selectMode {
	case HUNK:
		return s.selectionRangeForCurrentBlockOfChanges()
	case RANGE:
		if s.rangeStartLineIdx > s.selectedLineIdx {
			return s.selectedLineIdx, s.rangeStartLineIdx
		}
		return s.rangeStartLineIdx, s.selectedLineIdx
	case LINE:
		return s.selectedLineIdx, s.selectedLineIdx
	default:
		// should never happen
		return 0, 0
	}
}

// SelectedPatchRange returns the first and last patch-line index of the
// current selection. In split mode this is resolved through the currently
// selected column (range/hunk selection is column-locked - see
// nearestPatchLineIdxInColumn for why the endpoints are searched inward
// rather than read directly).
func (s *State) SelectedPatchRange() (int, int) {
	viewStart, viewEnd := s.SelectedViewRange()

	if !s.splitMode {
		return s.patchLineIndices[viewStart], s.patchLineIndices[viewEnd]
	}

	start := s.nearestPatchLineIdxInColumn(viewStart, viewEnd, s.selectedColumn)
	end := s.nearestPatchLineIdxInColumn(viewEnd, viewStart, s.selectedColumn)
	return start, end
}

// Returns the line indices of the selected patch range that are changes (i.e. additions or deletions)
func (s *State) LineIndicesOfAddedOrDeletedLinesInSelectedPatchRange() []int {
	viewStart, viewEnd := s.SelectedViewRange()
	lines := s.patch.Lines()

	indices := []int{}
	seen := map[int]bool{}
	for row := viewStart; row <= viewEnd; row++ {
		idx := s.strictPatchLineIdxAtRow(row, s.selectedColumn)
		if idx < 0 || seen[idx] {
			continue
		}
		seen[idx] = true
		if lines[idx].IsChange() {
			indices = append(indices, idx)
		}
	}
	return indices
}

func (s *State) CurrentLineNumber() int {
	return s.patch.LineNumberOfLine(s.GetSelectedPatchLineIdx())
}

func (s *State) AdjustSelectedLineIdx(change int) {
	s.DismissHunkSelectMode()
	s.SelectLine(s.selectedLineIdx + change)
}

func (s *State) RenderForLineIndices(includedLineIndices []int) string {
	includedLineIndicesSet := set.NewFromSlice(includedLineIndices)

	if s.splitMode {
		rendered, rows := s.patch.FormatSplitView(patch.FormatSplitViewOpts{
			IncLineIndices:  includedLineIndicesSet,
			ShowLineNumbers: s.showLineNumbers,
			Width:           s.viewWidth,
		})
		s.splitRows = rows
		s.rebuildSplitPatchIdxToRow()
		// A diff change can reuse this same State (see NewState) but with a
		// smaller patch than before; re-render is the first place that
		// would notice the row count shrank, so clamp defensively.
		s.selectedLineIdx = s.clampLineIdx(s.selectedLineIdx)
		s.rangeStartLineIdx = s.clampLineIdx(s.rangeStartLineIdx)
		s.fixSelectedColumn()
		return rendered
	}

	return s.patch.FormatView(patch.FormatViewOpts{
		IncLineIndices:  includedLineIndicesSet,
		ShowLineNumbers: s.showLineNumbers,
		Width:           s.viewWidth,
	})
}

func (s *State) PlainRenderSelected() string {
	firstLineIdx, lastLineIdx := s.SelectedPatchRange()
	return s.patch.FormatRangePlain(firstLineIdx, lastLineIdx)
}

func (s *State) SelectBottom() {
	s.DismissHunkSelectMode()
	s.SelectLine(s.rowCount() - 1)
}

func (s *State) SelectTop() {
	s.DismissHunkSelectMode()
	s.SelectLine(0)
}

func (s *State) CalculateOrigin(currentOrigin int, bufferHeight int, numLines int) int {
	firstLineIdx, lastLineIdx := s.SelectedViewRange()

	return calculateOrigin(currentOrigin, bufferHeight, numLines, firstLineIdx, lastLineIdx, s.GetSelectedViewLineIdx(), s.selectMode)
}

func wrapPatchLines(p *patch.Patch, gutterWidth int, view *gocui.View) ([]int, []int) {
	// Patch.FormatView adds the line-number gutter as a per-line prefix, not
	// part of the underlying diff text, so we reconstruct that same prefix
	// here and wrap it at the view's actual width. This has to match
	// FormatView's own gutter placement exactly (including which lines get
	// no gutter at all), or this wrapping computation and the view's own
	// wrapping of the real rendered content would disagree about where
	// lines break, desyncing the cursor from what's on screen.
	text := gutterPaddedText(p, gutterWidth)
	_, viewLineIndices, patchLineIndices := utils.WrapViewLinesToWidth(
		view.Wrap, view.Editable, text, view.InnerWidth(), view.TabWidth)
	return viewLineIndices, patchLineIndices
}

func gutterPaddedText(p *patch.Patch, gutterWidth int) string {
	lines := p.Lines()
	paddedLines := make([]string, len(lines))
	gutterPlaceholder := strings.Repeat(" ", gutterWidth)
	for i, line := range lines {
		// header and hunk-header lines get no gutter (see Patch.FormatView)
		if gutterWidth > 0 && line.Kind != patch.PATCH_HEADER && line.Kind != patch.HUNK_HEADER {
			paddedLines[i] = gutterPlaceholder + line.Content
		} else {
			paddedLines[i] = line.Content
		}
	}
	return strings.Join(paddedLines, "\n")
}

func (s *State) SelectNextStageableLineOfSameIncludedState(includedLines []int, included bool) {
	_, lastLineIdx := s.SelectedPatchRange()
	patchLineIdx, found := s.patch.GetNextChangeIdxOfSameIncludedState(lastLineIdx+1, includedLines, included)
	if found {
		s.SelectLine(s.rowForPatchIdx(patchLineIdx))
		s.setColumnForPatchIdx(patchLineIdx)
	}
}
