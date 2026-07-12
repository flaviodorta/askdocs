package postgres

import (
	"strconv"
	"strings"
)

// vectorLiteral renders a float32 slice in pgvector's text format ('[1,2,3]').
// Passed as a text parameter and cast with ::vector, which keeps pgx free of
// a pgvector type registration dependency.
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
