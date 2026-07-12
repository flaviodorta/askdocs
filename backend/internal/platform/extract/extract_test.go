package extract

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExtractPlainTextPassthrough(t *testing.T) {
	got, err := New().Extract(context.Background(), "text/plain", strings.NewReader("olá, texto puro"))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got != "olá, texto puro" {
		t.Errorf("text = %q, want passthrough", got)
	}
}

func TestExtractMarkdownPassthrough(t *testing.T) {
	got, err := New().Extract(context.Background(), "text/markdown", strings.NewReader("# título"))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if got != "# título" {
		t.Errorf("text = %q, want passthrough", got)
	}
}

func TestExtractUnknownTypeFails(t *testing.T) {
	if _, err := New().Extract(context.Background(), "image/png", strings.NewReader("")); err == nil {
		t.Fatal("Extract(image/png) error = nil, want no-extractor error")
	}
}

func TestExtractPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	f, err := os.Open("testdata/hello.pdf")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	got, err := New().Extract(context.Background(), "application/pdf", f)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !strings.Contains(got, "terminated within thirty days") {
		t.Errorf("text = %q, want fixture sentence", got)
	}
}

func TestExtractBrokenPDFSurfacesStderr(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	_, err := New().Extract(context.Background(), "application/pdf", strings.NewReader("not a pdf at all"))
	if err == nil || !strings.Contains(err.Error(), "pdftotext") {
		t.Fatalf("Extract(broken) error = %v, want pdftotext failure", err)
	}
}
