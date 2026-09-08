package patch

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/jesseduffield/lazygit/pkg/gui/style"
	"github.com/jesseduffield/lazygit/pkg/theme"
	"github.com/samber/lo"
)

// One syntax-highlighted source line, as an ordered sequence of styled spans
// whose concatenated text reconstructs the line's content exactly.
type highlightedLine []highlightSpan

type highlightSpan struct {
	text  string
	style style.TextStyle
}

// Computes per-hunk syntax highlighting for a patch. The result is indexed
// the same as hunks; each entry is indexed the same as that hunk's
// bodyLines, with a nil entry for lines with no highlighting available
// (including every line, if filename doesn't match a known language).
//
// Hunks are highlighted independently of each other, and only from the
// content visible within them (not the whole file), since that's all a diff
// gives us. A multi-line construct (e.g. a block comment) that starts above
// a hunk's visible context can therefore be mis-highlighted; this is an
// inherent limitation of highlighting a partial diff rather than a full
// file.
func highlightPatch(hunks []*Hunk, filename string) [][]highlightedLine {
	result := make([][]highlightedLine, len(hunks))

	if filename == "" {
		return result
	}
	lexer := lexers.Match(filename)
	if lexer == nil {
		return result
	}

	for i, hunk := range hunks {
		result[i] = highlightHunk(lexer, hunk)
	}
	return result
}

// A hunk mixes lines from two versions of the file (the old side: context +
// deletions, and the new side: context + additions). We tokenize each side
// as its own contiguous text so that constructs spanning multiple lines
// (e.g. a block comment) are lexed correctly within that side, then
// distribute the resulting per-line highlighting back to the hunk's body
// lines. Context lines appear on both sides with identical content, so we
// only need to take their highlighting from the old side.
func highlightHunk(lexer chroma.Lexer, hunk *Hunk) []highlightedLine {
	result := make([]highlightedLine, len(hunk.bodyLines))

	oldSideIndices := bodyLineIndicesWithKind(hunk, CONTEXT, DELETION)
	newSideIndices := bodyLineIndicesWithKind(hunk, ADDITION)

	distributeHighlighting(lexer, hunk, oldSideIndices, result)
	distributeHighlighting(lexer, hunk, newSideIndices, result)

	return result
}

func bodyLineIndicesWithKind(hunk *Hunk, kinds ...PatchLineKind) []int {
	indices := []int{}
	for i, line := range hunk.bodyLines {
		if lo.Contains(kinds, line.Kind) {
			indices = append(indices, i)
		}
	}
	return indices
}

func distributeHighlighting(lexer chroma.Lexer, hunk *Hunk, indices []int, result []highlightedLine) {
	if len(indices) == 0 {
		return
	}

	sourceLines := make([]string, len(indices))
	for i, idx := range indices {
		sourceLines[i] = lineContentWithoutSign(hunk.bodyLines[idx])
	}

	for i, highlighted := range tokenizeLines(lexer, sourceLines) {
		result[indices[i]] = highlighted
	}
}

func lineContentWithoutSign(line *PatchLine) string {
	if len(line.Content) == 0 {
		return ""
	}
	return line.Content[1:]
}

// Highlights lines (joined together as one source text so that multi-line
// constructs within it are lexed correctly) and splits the result back into
// one highlightedLine per input line. Always returns exactly len(lines)
// entries, falling back to nil entries if tokenising fails.
func tokenizeLines(lexer chroma.Lexer, lines []string) []highlightedLine {
	result := make([]highlightedLine, len(lines))
	if len(lines) == 0 {
		return result
	}

	text := strings.Join(lines, "\n") + "\n"
	tokens, err := chroma.Tokenise(lexer, nil, text)
	if err != nil {
		return result
	}

	lineIdx := 0
	current := highlightedLine{}
	flush := func() {
		if lineIdx < len(result) {
			result[lineIdx] = current
		}
		current = highlightedLine{}
		lineIdx++
	}

	for _, token := range tokens {
		tokenStyle := styleForTokenType(token.Type)
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				flush()
			}
			if part != "" {
				current = append(current, highlightSpan{text: part, style: tokenStyle})
			}
		}
	}
	flush()

	return result
}

// Maps a chroma token type to one of lazygit's basic (16-color) ANSI
// styles, rather than chroma's own themes, which use hardcoded RGB colors
// designed for a specific (usually dark) background. Basic colors are
// remapped by the terminal itself to whatever palette matches the user's
// light or dark scheme, the same way tools like `bat` degrade without
// terminal-background detection.
func styleForTokenType(t chroma.TokenType) style.TextStyle {
	switch {
	case t.InCategory(chroma.Keyword):
		return style.FgMagenta
	case t == chroma.NameFunction, t == chroma.NameFunctionMagic,
		t == chroma.NameClass, t == chroma.NameBuiltin, t == chroma.NameBuiltinPseudo:
		return style.FgBlue
	case t.InSubCategory(chroma.LiteralString):
		return style.FgYellow
	case t.InSubCategory(chroma.LiteralNumber):
		return style.FgCyan
	case t.InCategory(chroma.Comment):
		return style.FgBlackLighter
	default:
		return theme.DefaultTextColor
	}
}
