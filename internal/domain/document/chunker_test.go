package document

import (
	"strings"
	"testing"
)

func TestHeadingBreadcrumb(t *testing.T) {
	c := NewChunker(512, 80)
	chunks := c.Chunk([]Block{
		{Type: "heading", Text: "第一章", Level: 1},
		{Type: "paragraph", Text: "正文A"},
		{Type: "heading", Text: "第一节", Level: 2},
		{Type: "paragraph", Text: "正文B"},
		{Type: "heading", Text: "第二章", Level: 1},
		{Type: "paragraph", Text: "正文C"},
	})
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d: %+v", len(chunks), chunks)
	}
	if chunks[0].ContextHeader != "第一章" {
		t.Fatalf("header0 = %q", chunks[0].ContextHeader)
	}
	if chunks[1].ContextHeader != "第一章 > 第一节" {
		t.Fatalf("header1 = %q", chunks[1].ContextHeader)
	}
	if chunks[2].ContextHeader != "第二章" {
		t.Fatalf("header2 = %q", chunks[2].ContextHeader)
	}
	if chunks[0].RetrievalContent() != "第一章\n\n正文A" {
		t.Fatalf("retrieval content = %q", chunks[0].RetrievalContent())
	}
	if chunks[0].Seq != 0 || chunks[1].Seq != 1 || chunks[2].Seq != 2 {
		t.Fatalf("seq wrong: %+v", chunks)
	}
	if chunks[0].Start != 1 || chunks[0].End != 2 || chunks[1].Start != 3 || chunks[1].End != 4 {
		t.Fatalf("block range wrong: %+v", chunks)
	}
}

func TestRecursiveSplitKeepsSeparators(t *testing.T) {
	c := NewChunker(20, 4)
	text := "AAAAAAAAAA。BBBBBBBBBB。CCCCCCCCCC。DDDD"
	chunks := c.Chunk([]Block{{Type: "paragraph", Text: text}})
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, ch := range chunks {
		if runeLen(ch.Content) > 20 {
			t.Fatalf("oversize chunk %q (%d runes)", ch.Content, runeLen(ch.Content))
		}
	}
	if !strings.HasSuffix(chunks[0].Content, "。") {
		t.Fatalf("separator lost: chunk0 = %q", chunks[0].Content)
	}
}

func TestProtectedTableBlock(t *testing.T) {
	c := NewChunker(40, 4)
	table := "| 列A | 列B |\n| 1 | 2 |\n| 3 | 4 |"
	chunks := c.Chunk([]Block{
		{Type: "paragraph", Text: "前文前文前文"},
		{Type: "table", Text: table},
		{Type: "paragraph", Text: "后文后文后文"},
	})
	found := false
	for _, ch := range chunks {
		if strings.Contains(ch.Content, table) {
			found = true
		}
	}
	if !found {
		t.Fatalf("table was split: %+v", chunks)
	}
}

func TestOversizedTableHardSplit(t *testing.T) {
	c := NewChunker(10, 2)
	table := strings.Repeat("表格行内容", 4)
	chunks := c.Chunk([]Block{{Type: "table", Text: table}})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d: %+v", len(chunks), chunks)
	}
	total := 0
	for _, ch := range chunks {
		total += runeLen(ch.Content)
	}
	if total != 20 {
		t.Fatalf("content lost: total = %d", total)
	}
}

func TestOverlapBetweenParagraphs(t *testing.T) {
	c := NewChunker(16, 4)
	chunks := c.Chunk([]Block{
		{Type: "paragraph", Text: "AAAAAAAAAAAA"},
		{Type: "paragraph", Text: "BBBBBBBBBBBB"},
	})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	if chunks[0].Content != "AAAAAAAAAAAA" {
		t.Fatalf("chunk0 = %q", chunks[0].Content)
	}
	if !strings.HasPrefix(chunks[1].Content, "AAAA") {
		t.Fatalf("chunk1 = %q", chunks[1].Content)
	}
}

func TestProtectedBoundaryHasNoOverlap(t *testing.T) {
	c := NewChunker(16, 4)
	chunks := c.Chunk([]Block{
		{Type: "paragraph", Text: "AAAAAAAAAAAA"},
		{Type: "list", Text: "BBBBBBBBBBBB"},
	})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d: %+v", len(chunks), chunks)
	}
	if chunks[1].Content != "BBBBBBBBBBBB" {
		t.Fatalf("protected chunk has overlap: %q", chunks[1].Content)
	}
}

func TestEmpty(t *testing.T) {
	c := NewChunker(16, 4)
	if got := c.Chunk([]Block{{Type: "paragraph", Text: "  "}}); len(got) != 0 {
		t.Fatalf("got = %+v", got)
	}
}
