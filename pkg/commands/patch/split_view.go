package patch

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gookit/color"
	"github.com/jesseduffield/generics/set"
	"github.com/jesseduffield/lazygit/pkg/gui/style"
	"github.com/jesseduffield/lazygit/pkg/theme"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/rivo/uniseg"
)

// the column separator in the split view: a single-width box-drawing
// character (with a plain "|" fallback baked into the terminal driver for
// non-unicode terminals) flanked by a space on each side.
const splitDivider = " │ "

var splitDividerWidth = uniseg.StringWidth(splitDivider)

// dark, muted red/green row backgrounds for added/removed lines in the
// split view - color-matched to the hunk reference tool's own diff view
// (sampled: additions rgb(35,48,38), deletions rgb(52,38,38)). A plain RGB
// background here would force TextStyle to promote the whole style to
// 24-bit RGB the moment either its fg or bg is RGB (see deriveStyle), which
// would silently reinterpret patchLineStyle's basic-palette FgRed/FgGreen
// through gookit's fixed RGB approximation of "red"/"green" instead of the
// terminal's own theme colors - a real color shift, not a rendering quirk.
// NewFixedRGBColor avoids that promotion (see TextStyle.deriveMixedStyle),
// so the basic fg text keeps rendering in the terminal's own theme color
// while the background still gets this exact muted tint, distinct from
// style.BgGreen's brighter "included in patch" indicator.
var (
	splitViewAdditionBg = style.New().SetBg(style.NewFixedRGBColor(color.RGB(35, 48, 38, true)))
	splitViewDeletionBg = style.New().SetBg(style.NewFixedRGBColor(color.RGB(52, 38, 38, true)))
)

// splitViewRowBg returns the row background for a line, and whether one
// applies at all (context/header lines get none): the brighter "included in
// patch" indicator if included, else the kind-based tint above.
func splitViewRowBg(kind PatchLineKind, included bool) (style.TextStyle, bool) {
	if included {
		return style.BgGreen, true
	}
	switch kind {
	case ADDITION:
		return splitViewAdditionBg, true
	case DELETION:
		return splitViewDeletionBg, true
	default:
		return style.TextStyle{}, false
	}
}

type FormatSplitViewOpts struct {
	// if true, prefix each column's body lines with their own single-number
	// line-number gutter (unlike the unified view's combined two-number
	// gutter, each column only ever needs its own side's number)
	ShowLineNumbers bool
	// the width of the view the patch is being rendered into. Each column
	// gets (Width-len(splitDivider))/2, minus its own gutter width, to wrap
	// its content to. 0 means don't wrap at all (used by tests that don't
	// care about a real view's width).
	Width int
	// patch-line indices (Patch.Lines() indexing) that are currently
	// included in the patch being built - each cell whose own patch-line
	// index is in this set gets a green background tint across its whole
	// gutter+content, independently of the other column (there's no +/-
	// marker character in split mode to attach a narrower indicator to).
	IncLineIndices *set.Set[int]
}

// SplitRow describes one physical screen row of a FormatSplitView rendering:
// the patch-line index (in Patch.Lines() terms - the same indexing used for
// staging/selection) held by each column, or -1 if that column is blank on
// this row. A row whose cell content wraps to more than one physical row
// repeats the same indices on every continuation row, mirroring how the
// unified view maps several wrapped view lines back to one patch line.
//
// This is emitted by the renderer itself, rather than recomputed by a
// caller, so there's exactly one source of truth for "how many physical
// rows does this patch produce, and which patch line does each one hold" -
// recomputing it independently is exactly the kind of desync bug this
// package's wrap-bookkeeping regression test (see
// pkg/gui/patch_exploring/wrap_test.go) was written to catch.
type SplitRow struct {
	Old, New int
}

// Returns the patch as a string with ANSI color codes, laid out as two
// side-by-side columns (old content on the left, new content on the right)
// for rendering within a view, along with the physical-row mapping needed
// to drive an interactive cursor over it (see SplitRow).
func (self *Patch) FormatSplitView(opts FormatSplitViewOpts) (string, []SplitRow) {
	return formatSplitView(self, opts)
}

func formatSplitView(p *Patch, opts FormatSplitViewOpts) (string, []SplitRow) {
	if !p.ContainsChanges() {
		return "", nil
	}

	includedLineIndices := opts.IncLineIndices
	if includedLineIndices == nil {
		includedLineIndices = set.New[int]()
	}

	oldGutterWidth := p.SplitGutterWidth(opts.ShowLineNumbers, true)
	newGutterWidth := p.SplitGutterWidth(opts.ShowLineNumbers, false)

	// split the available width as evenly as possible between the two
	// columns; if it doesn't divide evenly, the extra column goes to the
	// right (new) side so the two together always add back up to the full
	// width rather than leaving an unused column.
	available := max(opts.Width-splitDividerWidth, 2)
	oldColumnWidth := max(available/2, 1)
	newColumnWidth := max(available-oldColumnWidth, 1)
	oldContentWidth := max(oldColumnWidth-oldGutterWidth, 1)
	newContentWidth := max(newColumnWidth-newGutterWidth, 1)

	b := &strings.Builder{}
	rows := []SplitRow{}

	for i, line := range p.header {
		styled := theme.DefaultTextColor.SetBold().Sprint(line)
		// pad (never truncate, matching the hunk header's existing
		// precedent) so a long header line can never wrap under gocui and
		// desync the row count above from what's actually on screen.
		if padding := opts.Width - len(line); padding > 0 {
			styled += strings.Repeat(" ", padding)
		}
		b.WriteString(styled)
		b.WriteString("\n")
		rows = append(rows, SplitRow{Old: i, New: i})
	}

	headerPresenter := &patchPresenter{plain: false, width: opts.Width}

	for hunkIdx, hunk := range p.hunks {
		hunkHeaderIdx := p.HunkStartIdx(hunkIdx)
		bodyStartIdx := hunkHeaderIdx + 1

		b.WriteString(headerPresenter.formatHunkHeaderLine(hunk))
		b.WriteString("\n")
		rows = append(rows, SplitRow{Old: hunkHeaderIdx, New: hunkHeaderIdx})

		highlighting := p.hunkHighlighting(hunkIdx)
		intralineDiffs := p.hunkIntralineDiffs(hunkIdx)
		oldNums, newNums := hunkLineNumbers(hunk)

		for _, row := range p.hunkSplitRows(hunkIdx) {
			oldCell := []cellLine{{}}
			oldIdx := -1
			if row.old != nil {
				oldIdx = bodyStartIdx + *row.old
				oldCell = renderSplitCell(hunk.bodyLines[*row.old], spansFor(highlighting, *row.old),
					intralineDiffFor(intralineDiffs, *row.old), oldNums[*row.old], oldGutterWidth, oldContentWidth,
					includedLineIndices.Includes(oldIdx))
			}
			newCell := []cellLine{{}}
			newIdx := -1
			if row.new != nil {
				newIdx = bodyStartIdx + *row.new
				newCell = renderSplitCell(hunk.bodyLines[*row.new], spansFor(highlighting, *row.new),
					intralineDiffFor(intralineDiffs, *row.new), newNums[*row.new], newGutterWidth, newContentWidth,
					includedLineIndices.Includes(newIdx))
			}

			height := max(len(oldCell), len(newCell))
			for i := range height {
				oldLine := cellLineAt(oldCell, i)
				newLine := cellLineAt(newCell, i)
				b.WriteString(oldLine.text)
				b.WriteString(strings.Repeat(" ", max(oldColumnWidth-oldLine.width, 0)))
				b.WriteString(splitDivider)
				b.WriteString(newLine.text)
				b.WriteString(strings.Repeat(" ", max(newColumnWidth-newLine.width, 0)))
				b.WriteString("\n")
				rows = append(rows, SplitRow{Old: oldIdx, New: newIdx})
			}
		}
	}

	return b.String(), rows
}

func spansFor(highlighting []highlightedLine, bodyLineIdx int) highlightedLine {
	if highlighting == nil {
		return nil
	}
	return highlighting[bodyLineIdx]
}

func intralineDiffFor(diffs []*byteRange, bodyLineIdx int) *byteRange {
	if diffs == nil {
		return nil
	}
	return diffs[bodyLineIdx]
}

// hunkLineNumbers returns, for every body line in the hunk, its old-file and
// new-file line number (0 for whichever side it doesn't correspond to).
// Computed once per hunk up front so that split rows can look up a line's
// number regardless of the order rows are visited in (which, once lines are
// paired up into rows, is no longer the same as hunk.bodyLines order for
// every line - see computeHunkSplitRows).
func hunkLineNumbers(hunk *Hunk) (oldNums []int, newNums []int) {
	oldNums = make([]int, len(hunk.bodyLines))
	newNums = make([]int, len(hunk.bodyLines))

	oldLine, newLine := hunk.oldStart, hunk.newStart
	for i, line := range hunk.bodyLines {
		switch line.Kind {
		case CONTEXT:
			oldNums[i], newNums[i] = oldLine, newLine
			oldLine++
			newLine++
		case ADDITION:
			newNums[i] = newLine
			newLine++
		case DELETION:
			oldNums[i] = oldLine
			oldLine++
		case PATCH_HEADER, HUNK_HEADER, NEWLINE_MESSAGE:
			// these don't correspond to a line in either file
		}
	}
	return oldNums, newNums
}

// cellLine is one physical (already word-wrapped) screen row of a single
// column's cell: the ANSI-styled text to print, and its visible width (so
// callers can pad it out to the column width without needing to re-measure
// styled text - which would require stripping ANSI codes back out).
type cellLine struct {
	text  string
	width int
}

func cellLineAt(cell []cellLine, i int) cellLine {
	if i < len(cell) {
		return cell[i]
	}
	return cellLine{}
}

// renderSplitCell renders a single body line into one column's cell,
// word-wrapping its content to contentWidth (which already excludes the
// gutter) and prefixing the first physical row with the line's
// single-number gutter (continuation rows get a blank gutter of the same
// width, matching how the unified view's gutter is only ever shown once per
// logical line). An added/removed line's row background (see
// splitViewRowBg - the brighter included-in-patch green if included, else a
// dark per-kind tint) is layered across the whole cell - gutter, content,
// and the padding out to contentWidth alike - on every wrapped continuation
// row too, so the tint reaches the divider with no unstyled gap.
func renderSplitCell(line *PatchLine, spans highlightedLine, changed *byteRange, lineNumber int, gutterWidth int, contentWidth int, included bool) []cellLine {
	rest := lineContentWithoutSign(line)
	lineStyle := patchLineStyle(line)
	finalSpans := applyChangeEmphasis(rest, spans, lineStyle, changed)
	rowBg, hasRowBg := splitViewRowBg(line.Kind, included)
	if hasRowBg {
		finalSpans = mergeStyleOntoSpans(finalSpans, rowBg)
	}

	gutter := formatSplitGutter(line.Kind, lineNumber, gutterWidth, included)
	blankGutter := strings.Repeat(" ", gutterWidth)
	if hasRowBg {
		blankGutter = rowBg.Sprint(blankGutter)
	}

	// WrapViewLinesToWidth expands tabs to spaces internally before wrapping,
	// so its returned chunks are substrings of a tab-expanded copy of rest,
	// not of rest itself. Expand tabs the same way here first (rather than
	// feeding rest to it directly) so chunk positions can be reliably found
	// below; wrapText's offsets are then mapped back to rest's via origOffset
	// before slicing finalSpans, which is keyed to rest's original offsets.
	wrapText, origOffset := expandTabsWithOffsetMap(rest, splitViewTabWidth)
	chunks, _, _ := utils.WrapViewLinesToWidth(true, false, wrapText, contentWidth, splitViewTabWidth)

	result := make([]cellLine, 0, len(chunks))
	pos := 0
	for i, chunk := range chunks {
		start := pos + strings.Index(wrapText[pos:], chunk)
		end := start + len(chunk)
		pos = end

		rowGutter := blankGutter
		if i == 0 {
			rowGutter = gutter
		}

		contentText := renderSpans(sliceSpans(finalSpans, origOffset[start], origOffset[end]))

		// pad out to contentWidth here (rather than leaving it to the
		// caller) so a row background reaches the full column width instead
		// of stopping at the text - otherwise the tint would leave a plain,
		// unstyled gap between the text and the divider.
		if padWidth := contentWidth - uniseg.StringWidth(chunk); padWidth > 0 {
			pad := strings.Repeat(" ", padWidth)
			if hasRowBg {
				pad = rowBg.Sprint(pad)
			}
			contentText += pad
		}

		result = append(result, cellLine{
			text:  rowGutter + contentText,
			width: gutterWidth + max(uniseg.StringWidth(chunk), contentWidth),
		})
	}
	return result
}

// splitViewTabWidth matches WrapViewLinesToWidth's own default (used when it
// receives a tabWidth < 1), so expandTabsWithOffsetMap's expansion agrees
// with what WrapViewLinesToWidth would have done to rest itself.
const splitViewTabWidth = 4

// expandTabsWithOffsetMap returns a copy of s with tabs expanded to spaces
// (mirroring WrapViewLinesToWidth's own per-line tab expansion exactly - tab
// stops are counted from the expanded output's own visual column, not from
// s's original byte index, so consecutive tabs still land on true 4-column
// stops - so the chunks it returns are substrings of the expanded copy), plus a map
// from each byte offset in the expanded copy back to the offset in s that
// produced it - length len(expanded)+1, with the final entry equal to
// len(s), so both a chunk's start and its (exclusive) end can be mapped.
func expandTabsWithOffsetMap(s string, tabWidth int) (string, []int) {
	if tabWidth < 1 {
		tabWidth = 4
	}

	var b strings.Builder
	offsets := make([]int, 0, len(s)+1)
	for i := range len(s) {
		if s[i] == '\t' {
			// tab stops are relative to the expanded output's current visual
			// column (b.Len()), not i (s's own, pre-expansion byte index) -
			// using i here would under-count every tab after the first one on
			// a line, since preceding tabs already expanded to more than one
			// column each.
			numSpaces := tabWidth - (b.Len() % tabWidth)
			for range numSpaces {
				offsets = append(offsets, i)
				b.WriteByte(' ')
			}
		} else {
			offsets = append(offsets, i)
			b.WriteByte(s[i])
		}
	}
	offsets = append(offsets, len(s))

	return b.String(), offsets
}

// formatSplitGutter formats a single-column line-number gutter cell (unlike
// format.go's formatGutter, which formats the unified view's combined
// two-number gutter).
func formatSplitGutter(kind PatchLineKind, lineNumber int, width int, included bool) string {
	if width == 0 {
		return ""
	}

	str := ""
	lineStyle := style.FgBlackLighter
	switch kind {
	case ADDITION:
		str = strconv.Itoa(lineNumber)
		lineStyle = style.FgGreen
	case DELETION:
		str = strconv.Itoa(lineNumber)
		lineStyle = style.FgRed
	case CONTEXT:
		str = strconv.Itoa(lineNumber)
	case NEWLINE_MESSAGE, HUNK_HEADER, PATCH_HEADER:
		// no line number for these
	}
	if rowBg, ok := splitViewRowBg(kind, included); ok {
		lineStyle = lineStyle.MergeStyle(rowBg)
	}

	// the trailing space is styled along with the number (rather than
	// appended unstyled) so an included cell's background tint isn't left
	// with a one-character gap between the gutter and the divider/content.
	return lineStyle.Sprint(fmt.Sprintf("%*s", width-1, str) + " ")
}

// mergeStyleOntoSpans layers extra onto every span's existing style (e.g.
// a background tint on top of whatever foreground color/emphasis a span
// already has).
func mergeStyleOntoSpans(spans highlightedLine, extra style.TextStyle) highlightedLine {
	result := make(highlightedLine, len(spans))
	for i, span := range spans {
		result[i] = highlightSpan{text: span.text, style: span.style.MergeStyle(extra)}
	}
	return result
}

// sliceSpans returns the portion of spans covering the byte range
// [start, end) of the text they reconstruct, preserving each span's
// existing style. Assumes spans' concatenated text reconstructs the full
// original text exactly (guaranteed by applyChangeEmphasis's return value,
// the only producer this is used with).
func sliceSpans(spans highlightedLine, start int, end int) highlightedLine {
	result := make(highlightedLine, 0, len(spans))
	pos := 0
	for _, span := range spans {
		spanStart := pos
		spanEnd := pos + len(span.text)
		pos = spanEnd

		clipStart := max(start, spanStart)
		clipEnd := min(end, spanEnd)
		if clipStart >= clipEnd {
			continue
		}

		result = append(result, highlightSpan{
			text:  span.text[clipStart-spanStart : clipEnd-spanStart],
			style: span.style,
		})
	}
	return result
}

func renderSpans(spans highlightedLine) string {
	b := &strings.Builder{}
	for _, span := range spans {
		b.WriteString(span.style.Sprint(span.text))
	}
	return b.String()
}

// A splitRow is one screen row of the side-by-side ("split") layout: the old
// (left column) and/or new (right column) body-line index it holds, indexed
// into the owning hunk's bodyLines. Either side may be nil, meaning that
// column is blank for this row (e.g. a pure addition has nothing on the old
// side).
type splitRow struct {
	old *int
	new *int
}

// Computes, for each hunk, the row layout of the side-by-side view: how many
// screen rows the hunk needs, and which body-line index (if any) each column
// of each row holds. This is the single source of truth for split-mode row
// counting - both the renderer (FormatSplitView) and, later, the interactive
// cursor need to agree on exactly this, or they'll desync (the same class of
// bug fixed elsewhere in this package for the unified view's line-wrap
// bookkeeping).
func computeSplitRows(hunks []*Hunk) [][]splitRow {
	result := make([][]splitRow, len(hunks))
	for i, hunk := range hunks {
		result[i] = computeHunkSplitRows(hunk.bodyLines)
	}
	return result
}

func computeHunkSplitRows(bodyLines []*PatchLine) []splitRow {
	rows := []splitRow{}

	i := 0
	for i < len(bodyLines) {
		switch bodyLines[i].Kind {
		case DELETION:
			delStart := i
			for i < len(bodyLines) && bodyLines[i].Kind == DELETION {
				i++
			}
			delEnd := i

			addStart := i
			for i < len(bodyLines) && bodyLines[i].Kind == ADDITION {
				i++
			}
			addEnd := i

			delCount := delEnd - delStart
			addCount := addEnd - addStart
			paired := min(delCount, addCount)

			for k := range paired {
				oldIdx := delStart + k
				newIdx := addStart + k
				rows = append(rows, splitRow{old: &oldIdx, new: &newIdx})
			}
			for k := paired; k < delCount; k++ {
				oldIdx := delStart + k
				rows = append(rows, splitRow{old: &oldIdx})
			}
			for k := paired; k < addCount; k++ {
				newIdx := addStart + k
				rows = append(rows, splitRow{new: &newIdx})
			}

		case ADDITION:
			// a run of additions with no immediately preceding deletions (a
			// pure addition block)
			addStart := i
			for i < len(bodyLines) && bodyLines[i].Kind == ADDITION {
				i++
			}
			for k := addStart; k < i; k++ {
				rows = append(rows, splitRow{new: &k})
			}

		default:
			// CONTEXT and NEWLINE_MESSAGE lines have identical content
			// regardless of which side you're looking at, so they're shown
			// mirrored in both columns, referencing the same body-line index.
			idx := i
			rows = append(rows, splitRow{old: &idx, new: &idx})
			i++
		}
	}

	return rows
}
