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
		appendLine(self.formatLineAux(line, theme.DefaultTextColor.SetBold(), false))
	}

	for _, hunk := range self.patch.hunks {
		appendLine(
			self.formatLineAux(
				hunk.formatHeaderStart(),
				style.FgCyan,
				false,
			) +
				// we're splitting the line into two parts: the diff header and the context
				// We explicitly pass 'included' as false for both because these are not part
				// of the actual patch
				self.formatLineAux(
					hunk.headerContext,
					theme.DefaultTextColor,
					false,
				),
		)

		oldLine, newLine := hunk.oldStart, hunk.newStart
		for _, line := range hunk.bodyLines {
			gutter := self.formatGutter(line.Kind, oldLine, newLine, oldGutterWidth, newGutterWidth)
			lineStyle := self.patchLineStyle(line)
			if line.IsChange() {
				appendLine(gutter + self.formatLine(line.Content, lineStyle, lineIdx))
			} else {
				appendLine(gutter + self.formatLineAux(line.Content, lineStyle, false))
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

func (self *patchPresenter) patchLineStyle(patchLine *PatchLine) style.TextStyle {
	switch patchLine.Kind {
	case ADDITION:
		return style.FgGreen
	case DELETION:
		return style.FgRed
	default:
		return theme.DefaultTextColor
	}
}

func (self *patchPresenter) formatLine(str string, textStyle style.TextStyle, index int) string {
	included := self.incLineIndices.Includes(index)

	return self.formatLineAux(str, textStyle, included)
}

// 'selected' means you've got it highlighted with your cursor
// 'included' means the line has been included in the patch (only applicable when
// building a patch)
func (self *patchPresenter) formatLineAux(str string, textStyle style.TextStyle, included bool) string {
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

	return firstCharStyle.Sprint(str[:1]) + textStyle.Sprint(str[1:])
}
