package document

import (
	"strings"
	"testing"
)

func TestChunkTextEmpty(t *testing.T) {
	if got := ChunkText("", 100, 20); got != nil {
		t.Errorf("ChunkText(empty) = %v, want nil", got)
	}
	if got := ChunkText("   \n\t  ", 100, 20); got != nil {
		t.Errorf("ChunkText(whitespace) = %v, want nil", got)
	}
}

func TestChunkTextSmallerThanSize(t *testing.T) {
	got := ChunkText("short text", 100, 20)
	if len(got) != 1 || got[0] != "short text" {
		t.Errorf("ChunkText = %v, want single original chunk", got)
	}
}

func TestChunkTextOverlap(t *testing.T) {
	// 26 letters, size 10, overlap 4 => starts at 0, 6, 12, 18, 24
	text := "abcdefghijklmnopqrstuvwxyz"
	got := ChunkText(text, 10, 4)

	want := []string{"abcdefghij", "ghijklmnop", "mnopqrstuv", "stuvwxyz"}
	// start=24 gives "yz", start=18 gives "stuvwxyz"; loop stops when end hits len
	if len(got) != len(want) {
		t.Fatalf("chunks = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestChunkTextNoSpacesHardCuts(t *testing.T) {
	text := strings.Repeat("x", 250)
	got := ChunkText(text, 100, 10)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 chunks (starts 0, 90, 180)", len(got))
	}
	if len(got[0]) != 100 || len(got[1]) != 100 || len(got[2]) != 70 {
		t.Errorf("chunk lens = %d,%d,%d, want 100,100,70", len(got[0]), len(got[1]), len(got[2]))
	}
}

func TestChunkTextHugeInputCoversEverything(t *testing.T) {
	text := strings.Repeat("palavra ", 50_000) // 400k runes
	got := ChunkText(text, 1000, 200)
	if len(got) == 0 {
		t.Fatal("no chunks for huge input")
	}
	last := got[len(got)-1]
	if !strings.HasSuffix(strings.TrimSpace(text), last) {
		t.Error("last chunk does not reach the end of the text")
	}
}

func TestChunkTextIsRuneSafe(t *testing.T) {
	text := strings.Repeat("ação§€", 100) // multi-byte runes
	for _, chunk := range ChunkText(text, 50, 10) {
		if !strings.Contains("ação§€", string([]rune(chunk)[0])) {
			t.Fatalf("chunk starts with unexpected rune: %q", chunk[:8])
		}
		for _, r := range chunk {
			if r == '�' {
				t.Fatalf("chunk contains replacement char — UTF-8 was cut: %q", chunk)
			}
		}
	}
}

func TestChunkTextInvalidParamsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ChunkText(size <= overlap) did not panic")
		}
	}()
	ChunkText("text", 10, 10)
}
