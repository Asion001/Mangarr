package downloads

import "testing"

func TestRemapPageAfterSplitting(t *testing.T) {
	// Original page 1 became three segments; page 2 stayed one page; page 3
	// became two segments.
	mapping := []int{0, 0, 0, 1, 2, 2}
	for _, tc := range []struct {
		page      int
		completed bool
		want      int
	}{
		{0, false, 0},
		{1, false, 1},
		{2, false, 4},
		{3, false, 5},
		{3, true, 6},
		{20, false, 6},
	} {
		if got := remapPage(tc.page, tc.completed, mapping); got != tc.want {
			t.Errorf("remapPage(%d, %v) = %d, want %d", tc.page, tc.completed, got, tc.want)
		}
	}
}
