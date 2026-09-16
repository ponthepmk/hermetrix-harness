package sum

import "testing"

func TestAdd(t *testing.T) {
	for _, tc := range []struct{ a, b, want int }{{2, 3, 5}, {-2, 3, 1}, {0, 0, 0}, {7, -4, 3}} {
		if got := Add(tc.a, tc.b); got != tc.want {
			t.Errorf("Add(%d,%d)=%d; want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
