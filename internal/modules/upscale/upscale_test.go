package upscale

import (
	"context"
	"slices"
	"testing"
)

func TestFitScale(t *testing.T) {
	cases := []struct {
		want   int
		scales []int
		got    int
	}{
		{2, []int{2, 3, 4}, 2},
		{3, []int{2, 4, 8}, 4},
		{2, []int{4}, 4},
		{8, []int{2, 3, 4}, 4},
		{4, []int{1}, 4},
	}
	for _, c := range cases {
		if got := FitScale(c.want, c.scales); got != c.got {
			t.Errorf("FitScale(%d, %v) = %d, want %d", c.want, c.scales, got, c.got)
		}
	}
}

func TestReportUsed(t *testing.T) {
	ReportUsed(context.Background(), "ignored") // no recorder: nothing happens
	ctx, used := WithUsed(context.Background())
	for _, m := range []string{"a", "b", "a", ""} {
		ReportUsed(ctx, m)
	}
	if got := used(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("used = %v", got)
	}
}
