package postgres

import "testing"

func TestVectorLiteral(t *testing.T) {
	cases := []struct {
		in   []float32
		want string
	}{
		{[]float32{}, "[]"},
		{[]float32{1}, "[1]"},
		{[]float32{0.5, -0.25, 3}, "[0.5,-0.25,3]"},
	}
	for _, c := range cases {
		if got := vectorLiteral(c.in); got != c.want {
			t.Errorf("vectorLiteral(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
