package document

import "strings"

// ChunkText splits text into chunks of at most size runes, consecutive chunks
// sharing overlap runes, so context at a boundary appears in both neighbors.
// Rune-based on purpose: byte slicing would cut UTF-8 sequences (accented
// Portuguese) in half. Panics if size <= overlap — that is a programming
// error, not an input error.
func ChunkText(text string, size, overlap int) []string {
	if size <= 0 || overlap < 0 || size <= overlap {
		panic("document: ChunkText requires size > overlap >= 0")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}

	step := size - overlap
	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := min(start+size, len(runes))
		if chunk := strings.TrimSpace(string(runes[start:end])); chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}
