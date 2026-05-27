package analysis

import "testing"

func TestLineForOffset(t *testing.T) {
	raw := []byte("abc\ndef\nghi")
	nls := computeNewlineOffsets(raw)
	cases := []struct {
		off  int64
		want int
	}{
		{0, 1}, {2, 1}, {3, 1}, {4, 2}, {7, 2}, {8, 3}, {10, 3},
	}
	for _, c := range cases {
		got := lineForOffset(c.off, nls)
		if got != c.want {
			t.Errorf("offset %d: got line %d, want %d", c.off, got, c.want)
		}
	}
}
