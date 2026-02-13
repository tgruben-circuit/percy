package memory

import (
	"strings"
)

// MessageText is a simplified message for chunking.
type MessageText struct {
	Role string
	Text string
}

// Chunk is a segment of text ready for indexing.
type Chunk struct {
	Text       string
	Index      int
	TokenCount int
}

// EstimateTokens returns a rough token count (~4 chars per token), rounding up.
func EstimateTokens(s string) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// roleLabel returns a display label for a message role.
func roleLabel(role string) string {
	switch role {
	case "user":
		return "User"
	case "agent", "assistant":
		return "Agent"
	default:
		if len(role) > 0 {
			return strings.ToUpper(role[:1]) + role[1:]
		}
		return role
	}
}

// ChunkMessages groups messages into chunks that fit within maxTokens.
// Each message is prefixed with its role label. Splitting happens at
// message boundaries — a single message is never split across chunks.
func ChunkMessages(messages []MessageText, maxTokens int) []Chunk {
	var chunks []Chunk
	var buf strings.Builder
	bufTokens := 0
	idx := 0

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		text := buf.String()
		chunks = append(chunks, Chunk{
			Text:       text,
			Index:      idx,
			TokenCount: EstimateTokens(text),
		})
		idx++
		buf.Reset()
		bufTokens = 0
	}

	for _, m := range messages {
		line := roleLabel(m.Role) + ": " + m.Text + "\n"
		lineTokens := EstimateTokens(line)

		// If adding this message would exceed the limit and we already
		// have content, flush the current buffer first.
		if bufTokens > 0 && bufTokens+lineTokens > maxTokens {
			flush()
		}

		buf.WriteString(line)
		bufTokens += lineTokens
	}
	flush()
	return chunks
}

// ChunkMarkdown splits markdown text by headings (lines starting with #),
// grouping sections to stay near maxTokens per chunk.
func ChunkMarkdown(md string, maxTokens int) []Chunk {
	sections := splitMarkdownSections(md)

	var chunks []Chunk
	var buf strings.Builder
	bufTokens := 0
	idx := 0

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		text := buf.String()
		chunks = append(chunks, Chunk{
			Text:       text,
			Index:      idx,
			TokenCount: EstimateTokens(text),
		})
		idx++
		buf.Reset()
		bufTokens = 0
	}

	for _, sec := range sections {
		secTokens := EstimateTokens(sec)
		if bufTokens > 0 && bufTokens+secTokens > maxTokens {
			flush()
		}
		buf.WriteString(sec)
		bufTokens += secTokens
	}
	flush()
	return chunks
}

// splitMarkdownSections splits markdown into sections at heading boundaries.
func splitMarkdownSections(md string) []string {
	lines := strings.Split(md, "\n")
	var sections []string
	var cur strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "#") && cur.Len() > 0 {
			sections = append(sections, cur.String())
			cur.Reset()
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	if cur.Len() > 0 {
		sections = append(sections, cur.String())
	}
	return sections
}
