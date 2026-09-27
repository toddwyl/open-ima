// Package document 承载文档聚合:实体、仓储契约、生命周期领域服务与分块策略。
// turns parsed blocks into retrieval chunks.
package media

import (
	"strings"
	"unicode/utf8"
)

type Block struct {
	Type  string
	Text  string
	Level int
}

type Chunk struct {
	Content       string
	ContextHeader string
	Seq           int
	Start         int
	End           int
}

func (c Chunk) RetrievalContent() string {
	body := strings.TrimSpace(c.Content)
	if c.ContextHeader == "" {
		return body
	}
	return c.ContextHeader + "\n\n" + body
}

type Chunker struct {
	size       int
	overlap    int
	separators []string
}

// NewChunker 创建按字符数切分的分块器。
func NewChunker(chunkSize, overlap int) *Chunker {
	return &Chunker{
		size:       chunkSize,
		overlap:    overlap,
		separators: []string{"\n\n", "\n", "。", "?", "!", ";", " "},
	}
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

type accumulator struct {
	buf          strings.Builder
	length       int
	firstBlock   int
	lastBlock    int
	hasContent   bool
	hasProtected bool
}

func (c *Chunker) Chunk(blocks []Block) []Chunk {
	var chunks []Chunk
	headings := make(map[int]string)
	header := func() string {
		parts := make([]string, 0, 6)
		for level := 1; level <= 6; level++ {
			if text, ok := headings[level]; ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " > ")
	}

	var current accumulator
	flush := func(withOverlap bool) {
		if !current.hasContent {
			return
		}
		content := current.buf.String()
		chunks = append(chunks, Chunk{
			Content:       content,
			ContextHeader: header(),
			Start:         current.firstBlock,
			End:           current.lastBlock + 1,
		})
		current = accumulator{firstBlock: current.lastBlock + 1}
		if withOverlap {
			tail := tailRunes(content, c.overlap)
			current.buf.WriteString(tail)
			current.length = runeLen(tail)
		}
	}
	appendBlock := func(index int, text string, protected bool) {
		if !current.hasContent && current.length == 0 {
			current.firstBlock = index
		}
		if current.hasContent {
			current.buf.WriteString("\n\n")
			current.length += 2
		}
		current.buf.WriteString(text)
		current.length += runeLen(text)
		current.lastBlock = index
		current.hasContent = true
		current.hasProtected = current.hasProtected || protected
	}

	for index, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		if block.Type == "heading" {
			flush(false)
			for level := range headings {
				if level >= block.Level {
					delete(headings, level)
				}
			}
			headings[block.Level] = text
			continue
		}

		protected := block.Type == "table" || block.Type == "list"
		if runeLen(text) > c.size {
			flush(false)
			for _, piece := range c.recursiveSplit(text, c.separators) {
				chunks = append(chunks, Chunk{
					Content:       piece,
					ContextHeader: header(),
					Start:         index,
					End:           index + 1,
				})
			}
			current = accumulator{firstBlock: index + 1}
			continue
		}

		additional := runeLen(text)
		if current.hasContent {
			additional += 2
		}
		if current.hasContent && current.length+additional > c.size {
			flush(!current.hasProtected && !protected)
		}
		appendBlock(index, text, protected)
	}
	flush(false)
	for index := range chunks {
		chunks[index].Seq = index
	}
	return chunks
}

func tailRunes(s string, count int) string {
	if count <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= count {
		return s
	}
	return string(runes[len(runes)-count:])
}

func (c *Chunker) recursiveSplit(text string, separators []string) []string {
	if runeLen(text) <= c.size {
		return []string{text}
	}
	if len(separators) == 0 {
		runes := []rune(text)
		pieces := make([]string, 0, (len(runes)+c.size-1)/c.size)
		for start := 0; start < len(runes); start += c.size {
			end := min(start+c.size, len(runes))
			pieces = append(pieces, string(runes[start:end]))
		}
		return pieces
	}

	separator := separators[0]
	rawParts := strings.Split(text, separator)
	parts := make([]string, len(rawParts))
	for index, part := range rawParts {
		if index < len(rawParts)-1 {
			parts[index] = part + separator
		} else {
			parts[index] = part
		}
	}

	var pieces []string
	var current strings.Builder
	currentLength := 0
	for _, part := range parts {
		partLength := runeLen(part)
		if currentLength+partLength <= c.size {
			current.WriteString(part)
			currentLength += partLength
			continue
		}
		if currentLength > 0 {
			pieces = append(pieces, current.String())
			current.Reset()
			currentLength = 0
		}
		if partLength > c.size {
			pieces = append(pieces, c.recursiveSplit(part, separators[1:])...)
			continue
		}
		current.WriteString(part)
		currentLength = partLength
	}
	if currentLength > 0 {
		pieces = append(pieces, current.String())
	}
	return pieces
}
