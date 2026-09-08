package patch

import (
	"testing"

	"github.com/jesseduffield/generics/set"
	"github.com/jesseduffield/lazygit/pkg/gui/style"
	"github.com/jesseduffield/lazygit/pkg/theme"
	"github.com/stretchr/testify/assert"
)

func TestCommonPrefixLen(t *testing.T) {
	assert.Equal(t, 4, commonPrefixLen("date    string", "date1   string"))
	assert.Equal(t, 0, commonPrefixLen("grape", "orange"))
	assert.Equal(t, 5, commonPrefixLen("hello", "hello"))
	assert.Equal(t, 0, commonPrefixLen("", "hello"))
	// snaps to a rune boundary rather than splitting the multi-byte 'é'
	assert.Equal(t, 1, commonPrefixLen("aé", "ab"))
}

func TestCommonSuffixLen(t *testing.T) {
	assert.Equal(t, 1, commonSuffixLen("grape", "orange"))
	assert.Equal(t, 5, commonSuffixLen("hello", "hello"))
	assert.Equal(t, 0, commonSuffixLen("", "hello"))
	// 'é' is a single rune but 2 UTF-8 bytes; the whole rune matches (not a
	// single byte of it), so the returned byte count reflects that
	assert.Equal(t, 2, commonSuffixLen("bé", "aé"))
}

func TestComputeHunkIntralineDiffsSingleCharacterInsertion(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: CONTEXT, Content: " commit  string"},
		{Kind: DELETION, Content: "-date    string"},
		{Kind: ADDITION, Content: "+date1   string"},
		{Kind: CONTEXT, Content: " version string"},
	}

	ranges := computeHunkIntralineDiffs(bodyLines)

	assert.Nil(t, ranges[0])
	assert.Nil(t, ranges[3])
	if assert.NotNil(t, ranges[1]) {
		assert.Equal(t, byteRange{start: 4, end: 5}, *ranges[1])
		assert.Equal(t, " ", "date    string"[ranges[1].start:ranges[1].end])
	}
	if assert.NotNil(t, ranges[2]) {
		assert.Equal(t, byteRange{start: 4, end: 5}, *ranges[2])
		assert.Equal(t, "1", "date1   string"[ranges[2].start:ranges[2].end])
	}
}

func TestComputeHunkIntralineDiffsSkipsUnequalCounts(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: DELETION, Content: "-one"},
		{Kind: DELETION, Content: "-two"},
		{Kind: ADDITION, Content: "+only"},
	}

	ranges := computeHunkIntralineDiffs(bodyLines)

	for i, r := range ranges {
		assert.Nilf(t, r, "expected no pairing for unequal deletion/addition counts, got a range at index %d", i)
	}
}

func TestComputeHunkIntralineDiffsMultipleBlocksInOneHunk(t *testing.T) {
	bodyLines := []*PatchLine{
		{Kind: DELETION, Content: "-foo"},
		{Kind: ADDITION, Content: "+foot"},
		{Kind: CONTEXT, Content: " unrelated"},
		{Kind: DELETION, Content: "-bar"},
		{Kind: ADDITION, Content: "+baz"},
	}

	ranges := computeHunkIntralineDiffs(bodyLines)

	assert.NotNil(t, ranges[0])
	assert.NotNil(t, ranges[1])
	assert.Nil(t, ranges[2])
	assert.NotNil(t, ranges[3])
	assert.NotNil(t, ranges[4])
}

func TestApplyChangeEmphasisPassthroughWhenNoChangeDetected(t *testing.T) {
	spans := highlightedLine{{text: "hello", style: style.FgBlue}}
	assert.Equal(t, spans, applyChangeEmphasis("hello", spans, style.FgGreen, nil))
	assert.Equal(t, spans, applyChangeEmphasis("hello", spans, style.FgGreen, &byteRange{start: 2, end: 2}))
}

func TestApplyChangeEmphasisWithoutSyntaxHighlighting(t *testing.T) {
	result := applyChangeEmphasis("date1   string", nil, style.FgGreen, &byteRange{start: 4, end: 5})

	assert.Equal(t, "date1   string", joinSpanTexts(result))
	assert.Equal(t, style.FgGreen, styleOfText(t, result, "date"))
	assert.Equal(t, style.FgGreen.SetBold().SetUnderline(), styleOfText(t, result, "1"))
	assert.Equal(t, style.FgGreen, styleOfText(t, result, "   string"))
}

func TestApplyChangeEmphasisSplitsAcrossSyntaxSpanBoundary(t *testing.T) {
	// "foo" and "Bar" are two separate syntax spans; the changed range spans
	// the tail of the first and the head of the second.
	spans := highlightedLine{
		{text: "foo", style: style.FgMagenta},
		{text: "Bar", style: style.FgBlue},
	}

	result := applyChangeEmphasis("fooBar", spans, style.FgGreen, &byteRange{start: 2, end: 4})

	assert.Equal(t, "fooBar", joinSpanTexts(result))
	assert.Equal(t, style.FgMagenta, styleOfText(t, result, "fo"))
	assert.Equal(t, style.FgMagenta.SetBold().SetUnderline(), styleOfText(t, result, "o"))
	assert.Equal(t, style.FgBlue.SetBold().SetUnderline(), styleOfText(t, result, "B"))
	assert.Equal(t, style.FgBlue, styleOfText(t, result, "ar"))
}

func TestFormatViewEmphasizesModifiedSubstring(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n commit  string\n-date    string\n+date1   string\n version string\n"

	t.Run("without syntax highlighting", func(t *testing.T) {
		p := Parse(diff) // filename left unset, so no syntax highlighting is available
		result := p.FormatView(FormatViewOpts{IncLineIndices: set.New[int]()})

		expectedOldChanged := style.FgRed.SetBold().SetUnderline().Sprint(" ")
		expectedNewChanged := style.FgGreen.SetBold().SetUnderline().Sprint("1")
		assert.Contains(t, result, expectedOldChanged)
		assert.Contains(t, result, expectedNewChanged)
		// the unchanged portion of the line must NOT be emphasized
		assert.NotContains(t, result, style.FgRed.SetBold().SetUnderline().Sprint("date"))
		assert.NotContains(t, result, style.FgGreen.SetBold().SetUnderline().Sprint("date"))
	})

	t.Run("with syntax highlighting", func(t *testing.T) {
		p := Parse(diff).SetFilename("f.go")
		result := p.FormatView(FormatViewOpts{IncLineIndices: set.New[int]()})

		expectedOldChanged := theme.DefaultTextColor.SetBold().SetUnderline().Sprint(" ")
		expectedNewChanged := theme.DefaultTextColor.SetBold().SetUnderline().Sprint("1")
		assert.Contains(t, result, expectedOldChanged)
		assert.Contains(t, result, expectedNewChanged)
	})
}

func TestFormatPlainUnaffectedByIntralineEmphasis(t *testing.T) {
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n commit  string\n-date    string\n+date1   string\n version string\n"

	p := Parse(diff)
	assert.Equal(t, diff, p.FormatPlain())
}
