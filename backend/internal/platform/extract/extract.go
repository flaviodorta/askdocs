// Package extract implements the document.TextExtractor port. PDFs go through
// the pdftotext binary (poppler-utils) — a machine dependency, but with far
// better extraction quality than the pure-Go alternatives.
package extract

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Extractor struct{}

func New() Extractor { return Extractor{} }

func (Extractor) Extract(ctx context.Context, contentType string, r io.Reader) (string, error) {
	switch contentType {
	case "application/pdf":
		return pdfToText(ctx, r)
	case "text/plain", "text/markdown":
		b, err := io.ReadAll(r)
		if err != nil {
			return "", fmt.Errorf("read text: %w", err)
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("no extractor for content type %q", contentType)
	}
}

func pdfToText(ctx context.Context, r io.Reader) (string, error) {
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", "-", "-")
	cmd.Stdin = r
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("pdftotext: %s", msg)
	}
	return out.String(), nil
}
