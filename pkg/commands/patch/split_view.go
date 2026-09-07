package patch

import (
	"fmt"
	"strconv"
	"strings"

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
}

// Returns the patch as a string with ANSI color codes, laid out as two
// side-by-side columns (old content on the left, new content on the right)
// for rendering within a view.
func (self *Patch) FormatSplitView(opts FormatSplitViewOpts) string {
	return formatSplitView(self, opts)
}

func formatSplitView(p *Patch, opts FormatSplitViewOpts) string {
	if !p.ContainsChanges() {
		return ""
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

	for _, line := range p.header {
		b.WriteString(theme.DefaultTextColor.SetBold().Sprint(line))
		b.WriteString("\n")
	}

	headerPresenter := &patchPresenter{plain: false, width: opts.Width}

	for hunkIdx, hunk := range p.hunks {
		b.WriteString(headerPresenter.formatHunkHeaderLine(hunk))
		b.WriteString("\n")

		highlighting := p.hunkHighlighting(hunkIdx)
		intralineDiffs := p.hunkIntralineDiffs(hunkIdx)
		oldNums, newNums := hunkLineNumbers(hunk)

		for _, row := range p.hunkSplitRows(hunkIdx) {
			oldCell := []cellLine{{}}
			if row.old != nil {
				oldCell = renderSplitCell(hunk.bodyLines[*row.old], spansFor(highlighting, *row.old),
					intralineDiffFor(intralineDiffs, *row.old), oldNums[*row.old], oldGutterWidth, oldContentWidth)
			}
			newCell := []cellLine{{}}
			if row.new != nil {
				newCell = renderSplitCell(hunk.bodyLines[*row.new], spansFor(highlighting, *row.new),
					intralineDiffFor(intralineDiffs, *row.new), newNums[*row.new], newGutterWidth, newContentWidth)
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
			}
		}
	}

	return b.String()
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
// logical line).
func renderSplitCell(line *PatchLine, spans highlightedLine, changed *byteRange, lineNumber int, gutterWidth int, contentWidth int) []cellLine {
	rest := lineContentWithoutSign(line)
	lineStyle := patchLineStyle(line)
	finalSpans := applyChangeEmphasis(rest, spans, lineStyle, changed)

	gutter := formatSplitGutter(line.Kind, lineNumber, gutterWidth)
	blankGutter := strings.Repeat(" ", gutterWidth)

	chunks, _, _ := utils.WrapViewLinesToWidth(true, false, rest, contentWidth, 0)

	result := make([]cellLine, 0, len(chunks))
	pos := 0
	for i, chunk := range chunks {
		start := pos + strings.Index(rest[pos:], chunk)
		end := start + len(chunk)
		pos = end

		rowGutter := blankGutter
		if i == 0 {
			rowGutter = gutter
		}

		contentText := renderSpans(sliceSpans(finalSpans, start, end))
		result = append(result, cellLine{
			text:  rowGutter + contentText,
			width: gutterWidth + uniseg.StringWidth(chunk),
		})
	}
	return result
}

// formatSplitGutter formats a single-column line-number gutter cell (unlike
// format.go's formatGutter, which formats the unified view's combined
// two-number gutter).
func formatSplitGutter(kind PatchLineKind, lineNumber int, width int) string {
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

	return lineStyle.Sprint(fmt.Sprintf("%*s", width-1, str)) + " "
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
