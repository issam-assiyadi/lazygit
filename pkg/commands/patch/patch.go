package patch

import (
	"strconv"
	"sync"

	"github.com/samber/lo"
)

type Patch struct {
	// header of the patch (split on newlines) e.g.
	// diff --git a/filename b/filename
	// index dcd3485..1ba5540 100644
	// --- a/filename
	// +++ b/filename
	header []string
	// hunks of the patch
	hunks []*Hunk
	// name of the file being diffed. Used for language-aware syntax
	// highlighting when rendering the patch for a view. May be empty, in
	// which case syntax highlighting is skipped.
	filename string

	// lazily computed the first time it's needed, since the same Patch is
	// typically rendered many times in a row (e.g. once per keystroke while
	// navigating it) without its content changing.
	highlightOnce sync.Once
	// per-hunk syntax highlighting; see highlightPatch.
	highlighting [][]highlightedLine

	// lazily computed, same rationale as highlightOnce above.
	intralineOnce sync.Once
	// per-hunk within-line diff ranges; see computeIntralineDiffs.
	intralineDiffs [][]*byteRange

	// lazily computed, same rationale as highlightOnce above.
	splitRowsOnce sync.Once
	// per-hunk split-view row layout; see computeSplitRows.
	splitRows [][]splitRow
}

// Records the name of the file being diffed, for use in syntax
// highlighting. Returns the patch itself so that it can be chained onto
// Parse().
func (self *Patch) SetFilename(filename string) *Patch {
	self.filename = filename
	return self
}

// Returns the syntax highlighting for the body lines of the hunk at the
// given index, indexed the same as that hunk's body lines, or nil if no
// highlighting is available (e.g. filename doesn't match a known language).
func (self *Patch) hunkHighlighting(hunkIdx int) []highlightedLine {
	self.highlightOnce.Do(func() {
		self.highlighting = highlightPatch(self.hunks, self.filename)
	})

	if hunkIdx < 0 || hunkIdx >= len(self.highlighting) {
		return nil
	}
	return self.highlighting[hunkIdx]
}

// Returns the within-line diff ranges for the body lines of the hunk at the
// given index, indexed the same as that hunk's body lines, or nil if the
// hunk index is out of range. Entries are nil for lines that aren't part of
// a detected 1:1 modification pair; see computeIntralineDiffs.
func (self *Patch) hunkIntralineDiffs(hunkIdx int) []*byteRange {
	self.intralineOnce.Do(func() {
		self.intralineDiffs = computeIntralineDiffs(self.hunks)
	})

	if hunkIdx < 0 || hunkIdx >= len(self.intralineDiffs) {
		return nil
	}
	return self.intralineDiffs[hunkIdx]
}

// Returns the split-view row layout for the hunk at the given index, or nil
// if the hunk index is out of range. See computeSplitRows: this is the
// single source of truth for split-mode row counting, shared by the
// renderer and (eventually) the interactive cursor.
func (self *Patch) hunkSplitRows(hunkIdx int) []splitRow {
	self.splitRowsOnce.Do(func() {
		self.splitRows = computeSplitRows(self.hunks)
	})

	if hunkIdx < 0 || hunkIdx >= len(self.splitRows) {
		return nil
	}
	return self.splitRows[hunkIdx]
}

// Returns a new patch with the specified transformation applied (e.g.
// only selecting a subset of changes).
// Leaves the original patch unchanged.
func (self *Patch) Transform(opts TransformOpts) *Patch {
	return transform(self, opts)
}

// Returns the patch as a plain string
func (self *Patch) FormatPlain() string {
	return formatPlain(self)
}

// Returns a range of lines from the patch as a plain string (range is inclusive)
func (self *Patch) FormatRangePlain(startIdx int, endIdx int) string {
	return formatRangePlain(self, startIdx, endIdx)
}

// Returns the patch as a string with ANSI color codes for displaying in a view
func (self *Patch) FormatView(opts FormatViewOpts) string {
	return formatView(self, opts)
}

// Returns the lines of the patch
func (self *Patch) Lines() []*PatchLine {
	lines := []*PatchLine{}
	for _, line := range self.header {
		lines = append(lines, &PatchLine{Content: line, Kind: PATCH_HEADER})
	}

	for _, hunk := range self.hunks {
		lines = append(lines, hunk.allLines()...)
	}

	return lines
}

// Returns the old-file starting line number of the hunk containing the given
// patch line index. Returns 0 if the line is not inside any hunk.
func (self *Patch) HunkOldStartForLine(idx int) int {
	hunkIdx := self.HunkContainingLine(idx)
	if hunkIdx == -1 {
		return 0
	}
	return self.hunks[hunkIdx].oldStart
}

// Returns the patch line index of the first line in the given hunk
func (self *Patch) HunkStartIdx(hunkIndex int) int {
	hunkIndex = lo.Clamp(hunkIndex, 0, len(self.hunks)-1)

	result := len(self.header)
	for i := range hunkIndex {
		result += self.hunks[i].lineCount()
	}
	return result
}

// Returns the patch line index of the last line in the given hunk
func (self *Patch) HunkEndIdx(hunkIndex int) int {
	hunkIndex = lo.Clamp(hunkIndex, 0, len(self.hunks)-1)

	return self.HunkStartIdx(hunkIndex) + self.hunks[hunkIndex].lineCount() - 1
}

func (self *Patch) ContainsChanges() bool {
	return lo.SomeBy(self.hunks, func(hunk *Hunk) bool {
		return hunk.containsChanges()
	})
}

// Takes a line index in the patch and returns the line number in the new file.
// If the line is a header line, returns 1.
// If the line is a hunk header line, returns the first file line number in that hunk.
// If the line is out of range below, returns the last file line number in the last hunk.
func (self *Patch) LineNumberOfLine(idx int) int {
	if idx < len(self.header) || len(self.hunks) == 0 {
		return 1
	}

	hunkIdx := self.HunkContainingLine(idx)
	// cursor out of range, return last file line number
	if hunkIdx == -1 {
		lastHunk := self.hunks[len(self.hunks)-1]
		return lastHunk.newStart + lastHunk.newLength() - 1
	}

	hunk := self.hunks[hunkIdx]
	hunkStartIdx := self.HunkStartIdx(hunkIdx)
	idxInHunk := idx - hunkStartIdx

	if idxInHunk == 0 {
		return hunk.newStart
	}

	lines := hunk.bodyLines[:idxInHunk-1]
	offset := nLinesWithKind(lines, []PatchLineKind{ADDITION, CONTEXT})
	return hunk.newStart + offset
}

// Returns hunk index containing the line at the given patch line index
func (self *Patch) HunkContainingLine(idx int) int {
	for hunkIdx, hunk := range self.hunks {
		hunkStartIdx := self.HunkStartIdx(hunkIdx)
		if idx >= hunkStartIdx && idx < hunkStartIdx+hunk.lineCount() {
			return hunkIdx
		}
	}
	return -1
}

// Returns the patch line index of the next change (i.e. addition or deletion)
// that matches the same "included" state, given the includedLines. If you don't
// care about included states, pass nil for includedLines and false for included.
func (self *Patch) GetNextChangeIdxOfSameIncludedState(idx int, includedLines []int, included bool) (int, bool) {
	idx = lo.Clamp(idx, 0, self.LineCount()-1)

	lines := self.Lines()

	isMatch := func(i int, line *PatchLine) bool {
		sameIncludedState := lo.Contains(includedLines, i) == included
		return line.IsChange() && sameIncludedState
	}

	for i, line := range lines[idx:] {
		if isMatch(i+idx, line) {
			return i + idx, true
		}
	}

	// there are no changes from the cursor onwards so we'll instead
	// return the index of the last change
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if isMatch(i, line) {
			return i, true
		}
	}

	return 0, false
}

// Returns the patch line index of the next change (i.e. addition or deletion).
func (self *Patch) GetNextChangeIdx(idx int) int {
	result, _ := self.GetNextChangeIdxOfSameIncludedState(idx, nil, false)
	return result
}

// Returns the length of the patch in lines
func (self *Patch) LineCount() int {
	count := len(self.header)
	for _, hunk := range self.hunks {
		count += hunk.lineCount()
	}
	return count
}

// Returns the number of hunks of the patch
func (self *Patch) HunkCount() int {
	return len(self.hunks)
}

// Adjust the given line number (one-based) according to the current patch. The
// patch is supposed to be a diff of an old file state against the working
// directory; the line number is a line number in that old file, and the
// function returns the corresponding line number in the working directory file.
func (self *Patch) AdjustLineNumber(lineNumber int) int {
	adjustedLineNumber := lineNumber
	for _, hunk := range self.hunks {
		if hunk.oldStart >= lineNumber {
			break
		}

		if hunk.oldStart+hunk.oldLength() > lineNumber {
			return hunk.newStart
		}

		adjustedLineNumber += hunk.newLength() - hunk.oldLength()
	}

	return adjustedLineNumber
}

// Returns the number of digits needed to display the largest old-file and
// new-file line numbers that occur anywhere in the patch (0 for a side that
// never shows a line number, e.g. the old side of a diff against an empty
// file).
func (self *Patch) LineNumberColumnWidths() (oldWidth, newWidth int) {
	maxOld, maxNew := 0, 0
	for _, hunk := range self.hunks {
		if oldLength := hunk.oldLength(); oldLength > 0 {
			maxOld = max(maxOld, hunk.oldStart+oldLength-1)
		}
		if newLength := hunk.newLength(); newLength > 0 {
			maxNew = max(maxNew, hunk.newStart+newLength-1)
		}
	}
	if maxOld > 0 {
		oldWidth = len(strconv.Itoa(maxOld))
	}
	if maxNew > 0 {
		newWidth = len(strconv.Itoa(maxNew))
	}
	return oldWidth, newWidth
}

// Returns the total width of the line-number gutter rendered by FormatView
// when ShowLineNumbers is set, or 0 if line numbers aren't shown (either
// because showLineNumbers is false, or because the patch has no hunks to
// derive column widths from).
func (self *Patch) GutterWidth(showLineNumbers bool) int {
	if !showLineNumbers {
		return 0
	}

	oldWidth, newWidth := self.LineNumberColumnWidths()
	if oldWidth == 0 && newWidth == 0 {
		return 0
	}

	// one column per side, plus a trailing space after each
	return oldWidth + 1 + newWidth + 1
}

// Returns the width of a single-column line-number gutter for one side (old
// or new) of the split view, or 0 if line numbers aren't shown. Unlike
// GutterWidth (which returns a combined old+new width for the unified
// view's single two-number gutter), split mode renders old and new in
// separate columns, each needing only its own side's digit width.
func (self *Patch) SplitGutterWidth(showLineNumbers bool, oldSide bool) int {
	if !showLineNumbers {
		return 0
	}

	oldWidth, newWidth := self.LineNumberColumnWidths()
	width := newWidth
	if oldSide {
		width = oldWidth
	}
	if width == 0 {
		return 0
	}

	// one column plus a trailing space
	return width + 1
}

func (self *Patch) IsSingleHunkForWholeFile() bool {
	if len(self.hunks) != 1 {
		return false
	}

	// We consider a patch to be a single hunk for the whole file if it has only additions or
	// deletions but not both, and no context lines. This not quite correct, because it will also
	// return true for a block of added or deleted lines if the diff context size is 0, but in this
	// case you wouldn't be able to stage things anyway, so it doesn't matter.
	bodyLines := self.hunks[0].bodyLines
	return nLinesWithKind(bodyLines, []PatchLineKind{DELETION, CONTEXT}) == 0 ||
		nLinesWithKind(bodyLines, []PatchLineKind{ADDITION, CONTEXT}) == 0
}
