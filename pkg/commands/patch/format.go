package patch

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jesseduffield/generics/set"
	"github.com/jesseduffield/lazygit/pkg/gui/style"
	"github.com/jesseduffield/lazygit/pkg/theme"
	"github.com/samber/lo"
)

type patchPresenter struct {
	patch *Patch
	// if true, all following fields are ignored
	plain bool

	// line indices for tagged lines (e.g. lines added to a custom patch)
	incLineIndices *set.Set[int]

	// if true, prefix each body line with an old/new line-number gutter
	showLineNumbers bool

	// the width to pad hunk header lines' background out to, so the band
	// spans the whole view rather than just the header text. 0 means don't
	// pad (used e.g. by tests that don't care about a real view's width).
	width int
}

// formats the patch as a plain string
func formatPlain(patch *Patch) string {
	presenter := &patchPresenter{
		patch:          patch,
		plain:          true,
		incLineIndices: set.New[int](),
	}
	return presenter.format()
}

func formatRangePlain(patch *Patch, startIdx int, endIdx int) string {
	lines := patch.Lines()[startIdx : endIdx+1]
	return strings.Join(
		lo.Map(lines, func(line *PatchLine, _ int) string {
			return line.Content + "\n"
		}),
		"",
	)
}

type FormatViewOpts struct {
	// line indices for tagged lines (e.g. lines added to a custom patch)
	IncLineIndices *set.Set[int]
	// if true, prefix each body line with an old/new line-number gutter
	ShowLineNumbers bool
	// the width of the view the patch is being rendered into, used to pad
	// hunk header lines' background so it spans the whole view. 0 (the
	// zero value) means don't pad.
	Width int
}

// formats the patch for rendering within a view, meaning it's coloured and
// highlights selected items
func formatView(patch *Patch, opts FormatViewOpts) string {
	includedLineIndices := opts.IncLineIndices
	if includedLineIndices == nil {
		includedLineIndices = set.New[int]()
	}
	presenter := &patchPresenter{
		patch:           patch,
		plain:           false,
		incLineIndices:  includedLineIndices,
		showLineNumbers: opts.ShowLineNumbers,
		width:           opts.Width,
	}
	return presenter.format()
}

func (self *patchPresenter) format() string {
	// if we have no changes in our patch (i.e. no additions or deletions) then
	// the patch is effectively empty and we can return an empty string
	if !self.patch.ContainsChanges() {
		return ""
	}

	oldGutterWidth, newGutterWidth := 0, 0
	if self.showLineNumbers && !self.plain {
		oldGutterWidth, newGutterWidth = self.patch.LineNumberColumnWidths()
	}

	stringBuilder := &strings.Builder{}
	lineIdx := 0
	appendLine := func(line string) {
		_, _ = stringBuilder.WriteString(line + "\n")

		lineIdx++
	}

	for _, line := range self.patch.header {
		// always passing false for 'included' here because header lines are not part of the patch
		appendLine(self.formatLineAux(line, theme.DefaultTextColor.SetBold(), false, nil))
	}

	for hunkIdx, hunk := range self.patch.hunks {
		appendLine(self.formatHunkHeaderLine(hunk))

		var highlighting []highlightedLine
		var intralineDiffs []*byteRange
		if !self.plain {
			highlighting = self.patch.hunkHighlighting(hunkIdx)
			intralineDiffs = self.patch.hunkIntralineDiffs(hunkIdx)
		}

		oldLine, newLine := hunk.oldStart, hunk.newStart
		for bodyLineIdx, line := range hunk.bodyLines {
			gutter := self.formatGutter(line.Kind, oldLine, newLine, oldGutterWidth, newGutterWidth)
			lineStyle := patchLineStyle(line)
			var spans highlightedLine
			if highlighting != nil {
				spans = highlighting[bodyLineIdx]
			}
			if line.IsChange() {
				var changed *byteRange
				if intralineDiffs != nil {
					changed = intralineDiffs[bodyLineIdx]
				}
				spans = applyChangeEmphasis(lineContentWithoutSign(line), spans, lineStyle, changed)
				appendLine(gutter + self.formatLine(line.Content, lineStyle, lineIdx, spans))
			} else {
				appendLine(gutter + self.formatLineAux(line.Content, lineStyle, false, spans))
			}

			switch line.Kind {
			case CONTEXT:
				oldLine++
				newLine++
			case ADDITION:
				newLine++
			case DELETION:
				oldLine++
			case PATCH_HEADER, HUNK_HEADER, NEWLINE_MESSAGE:
				// these don't correspond to a line in either file
			}
		}
	}

	return stringBuilder.String()
}

// formats the old/new line-number gutter for a single body line. oldWidth and
// newWidth are the column widths returned by Patch.LineNumberColumnWidths;
// formatGutter returns "" for both of them being zero, which happens when
// line numbers aren't being shown at all.
func (self *patchPresenter) formatGutter(kind PatchLineKind, oldLine int, newLine int, oldWidth int, newWidth int) string {
	if oldWidth == 0 && newWidth == 0 {
		return ""
	}

	oldStr, newStr := "", ""
	oldStyle, newStyle := style.FgBlackLighter, style.FgBlackLighter

	switch kind {
	case ADDITION:
		newStr = strconv.Itoa(newLine)
		newStyle = style.FgGreen
	case DELETION:
		oldStr = strconv.Itoa(oldLine)
		oldStyle = style.FgRed
	case CONTEXT:
		oldStr = strconv.Itoa(oldLine)
		newStr = strconv.Itoa(newLine)
	case NEWLINE_MESSAGE, HUNK_HEADER, PATCH_HEADER:
		// no line numbers for these
	}

	return oldStyle.Sprint(fmt.Sprintf("%*s", oldWidth, oldStr)) + " " +
		newStyle.Sprint(fmt.Sprintf("%*s", newWidth, newStr)) + " "
}

// formats a hunk's "@@ -a,b +c,d @@ context" header line. The whole line
// (padded out to self.width, if set) gets a subtle background so it reads
// as a section divider spanning the view, rather than just another colored
// line among the body lines.
func (self *patchPresenter) formatHunkHeaderLine(hunk *Hunk) string {
	if self.plain {
		return hunk.formatHeaderLine()
	}

	numbersStyle := style.FgCyan.SetBold().MergeStyle(style.BgBlackLighter)
	contextStyle := theme.DefaultTextColor.MergeStyle(style.BgBlackLighter)

	headerLine := hunk.formatHeaderLine()
	formatted := numbersStyle.Sprint(hunk.formatHeaderStart()) + contextStyle.Sprint(hunk.headerContext)

	if padding := self.width - len(headerLine); padding > 0 {
		formatted += contextStyle.Sprint(strings.Repeat(" ", padding))
	}

	return formatted
}

func patchLineStyle(patchLine *PatchLine) style.TextStyle {
	switch patchLine.Kind {
	case ADDITION:
		return style.FgGreen
	case DELETION:
		return style.FgRed
	default:
		return theme.DefaultTextColor
	}
}

func (self *patchPresenter) formatLine(str string, textStyle style.TextStyle, index int, spans highlightedLine) string {
	included := self.incLineIndices.Includes(index)

	return self.formatLineAux(str, textStyle, included, spans)
}

// 'selected' means you've got it highlighted with your cursor
// 'included' means the line has been included in the patch (only applicable when
// building a patch)
// 'spans' is the line's syntax highlighting, if any is available; it covers
// everything in str after the leading +/-/space character.
func (self *patchPresenter) formatLineAux(str string, textStyle style.TextStyle, included bool, spans highlightedLine) string {
	if self.plain {
		return str
	}

	firstCharStyle := textStyle
	if included {
		firstCharStyle = firstCharStyle.MergeStyle(style.BgGreen)
	}

	if len(str) < 2 {
		return firstCharStyle.Sprint(str)
	}

	rest := str[1:]
	if formatted, ok := formatHighlightedContent(rest, spans); ok {
		return firstCharStyle.Sprint(str[:1]) + formatted
	}

	return firstCharStyle.Sprint(str[:1]) + textStyle.Sprint(rest)
}

// Renders rest using its syntax-highlighting spans, provided they exactly
// reconstruct it (guarding against any lexer/reconstruction mismatch).
// Returns ok=false if spans don't apply, in which case the caller should
// fall back to uniform coloring.
func formatHighlightedContent(rest string, spans highlightedLine) (string, bool) {
	if spans == nil {
		return "", false
	}

	var textBuilder strings.Builder
	for _, span := range spans {
		textBuilder.WriteString(span.text)
	}
	if textBuilder.String() != rest {
		return "", false
	}

	var result strings.Builder
	for _, span := range spans {
		result.WriteString(span.style.Sprint(span.text))
	}
	return result.String(), true
}
