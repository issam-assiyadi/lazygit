package patch

import (
	"strings"
	"unicode/utf8"

	"github.com/jesseduffield/lazygit/pkg/gui/style"
)

// byteRange is a half-open [start, end) byte range within a line's content
// (after the leading +/-/space marker), identifying the portion that
// differs from its paired line on the other side of a modification. A nil
// *byteRange means "this line isn't part of a detected modification pair".
type byteRange struct {
	start, end int
}

func (r *byteRange) empty() bool {
	return r == nil || r.start >= r.end
}

// Computes, for each hunk, the within-line changed byte range for each body
// line that's part of a "pure modification" block: a run of N deletions
// immediately followed by a run of N additions (no context or other lines
// in between). Lines outside such a block get a nil entry, meaning no
// intra-line emphasis applies to them - they render as plain whole-line
// additions/deletions, same as before this feature existed.
//
// We deliberately don't try to pair up blocks with unequal deletion/addition
// counts, or otherwise infer which old line "became" which new line: that
// requires the kind of homology-scoring algorithm delta/diff-highlight use,
// which is out of scope here. A 1:1 zip in order (in the order the lines
// appear) covers the common case of modifying existing lines in place.
func computeIntralineDiffs(hunks []*Hunk) [][]*byteRange {
	result := make([][]*byteRange, len(hunks))
	for i, hunk := range hunks {
		result[i] = computeHunkIntralineDiffs(hunk.bodyLines)
	}
	return result
}

func computeHunkIntralineDiffs(bodyLines []*PatchLine) []*byteRange {
	ranges := make([]*byteRange, len(bodyLines))

	i := 0
	for i < len(bodyLines) {
		if bodyLines[i].Kind != DELETION {
			i++
			continue
		}

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
		if delCount != addCount {
			continue
		}

		for k := range delCount {
			oldLine := bodyLines[delStart+k]
			newLine := bodyLines[addStart+k]
			oldRange, newRange := diffRanges(lineContentWithoutSign(oldLine), lineContentWithoutSign(newLine))
			ranges[delStart+k] = oldRange
			ranges[addStart+k] = newRange
		}
	}

	return ranges
}

// diffRanges returns the byte ranges within oldRest and newRest that differ,
// found by trimming the longest common prefix and then the longest common
// suffix of the remainder. Both trims snap to rune boundaries so a
// multi-byte character is never split across the changed/unchanged
// boundary.
func diffRanges(oldRest string, newRest string) (*byteRange, *byteRange) {
	prefixLen := commonPrefixLen(oldRest, newRest)
	suffixLen := commonSuffixLen(oldRest[prefixLen:], newRest[prefixLen:])

	return &byteRange{start: prefixLen, end: len(oldRest) - suffixLen},
		&byteRange{start: prefixLen, end: len(newRest) - suffixLen}
}

func commonPrefixLen(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) {
		ra, sizeA := utf8.DecodeRuneInString(a[i:])
		rb, _ := utf8.DecodeRuneInString(b[i:])
		if ra != rb {
			break
		}
		i += sizeA
	}
	return i
}

// commonSuffixLen returns the number of trailing bytes shared by a and b.
// Because a match requires identical runes (which always have identical
// UTF-8 byte width), the byte count consumed from each side is always the
// same, so a single length applies to both.
func commonSuffixLen(a, b string) int {
	i, j := len(a), len(b)
	for i > 0 && j > 0 {
		ra, sizeA := utf8.DecodeLastRuneInString(a[:i])
		rb, sizeB := utf8.DecodeLastRuneInString(b[:j])
		if ra != rb {
			break
		}
		i -= sizeA
		j -= sizeB
	}
	return len(a) - i
}

// emphasize is layered on top of whatever style a changed span would
// otherwise have (syntax color, or the line's plain add/delete color), to
// make the actually-changed portion of a modified line stand out from the
// unchanged portion around it.
func emphasize(base style.TextStyle) style.TextStyle {
	return base.SetBold().SetUnderline()
}

// applyChangeEmphasis takes the spans that would otherwise render `rest`
// (syntax-highlighting spans if available, or nil), and the style to fall
// back to when there's none, and returns a span list that's always safe to
// render: if spans is nil or doesn't reconstruct rest exactly, it's
// replaced with a single span covering all of rest in baseStyle. On top of
// that, if changed is non-nil and non-empty (this line is part of a
// detected 1:1 modification pair), emphasize is layered onto the portion of
// the result inside that range.
func applyChangeEmphasis(rest string, spans highlightedLine, baseStyle style.TextStyle, changed *byteRange) highlightedLine {
	base := spans
	if !spansReconstruct(base, rest) {
		base = highlightedLine{{text: rest, style: baseStyle}}
	}

	if changed.empty() {
		return base
	}

	return splitAndEmphasize(base, changed.start, changed.end)
}

func spansReconstruct(spans highlightedLine, rest string) bool {
	if spans == nil {
		return false
	}
	var b strings.Builder
	for _, span := range spans {
		b.WriteString(span.text)
	}
	return b.String() == rest
}

// splitAndEmphasize walks spans (whose concatenated text is assumed to
// reconstruct the line exactly) and splits any span straddling the
// [start, end) boundary, so the sub-range inside it can get emphasize
// layered on top of its existing style while the rest of the span keeps
// its style unchanged.
func splitAndEmphasize(spans highlightedLine, start, end int) highlightedLine {
	result := make(highlightedLine, 0, len(spans)+2)
	pos := 0
	for _, span := range spans {
		spanStart := pos
		spanEnd := pos + len(span.text)
		pos = spanEnd

		clipStart := max(start, spanStart)
		clipEnd := min(end, spanEnd)

		if clipStart >= clipEnd {
			result = append(result, span)
			continue
		}

		if clipStart > spanStart {
			result = append(result, highlightSpan{
				text:  span.text[:clipStart-spanStart],
				style: span.style,
			})
		}

		result = append(result, highlightSpan{
			text:  span.text[clipStart-spanStart : clipEnd-spanStart],
			style: emphasize(span.style),
		})

		if clipEnd < spanEnd {
			result = append(result, highlightSpan{
				text:  span.text[clipEnd-spanStart:],
				style: span.style,
			})
		}
	}
	return result
}
