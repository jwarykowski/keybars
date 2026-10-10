package bars

import (
	"slices"
	"testing"
	"time"
)

func TestPeaks(t *testing.T) {
	p := NewPeaks(200*time.Millisecond, 10)
	t0 := time.Unix(0, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

	if got := p.Update([]int{4, 1}, at(0)); !slices.Equal(got, []int{4, 1}) {
		t.Fatalf("peaks jump to the bars: got %v", got)
	}
	if got := p.Update([]int{0, 2}, at(100)); !slices.Equal(got, []int{4, 2}) {
		t.Errorf("within the hold the peak stays, a taller bar pushes it up: got %v", got)
	}
	// hold over at 200ms; 100ms later the first peak has fallen 1 level
	p.Update([]int{0, 2}, at(250))
	if got := p.Update([]int{0, 2}, at(350)); got[0] != 3 {
		t.Errorf("falling at 10 levels/s: got %v, want 3", got[0])
	}
	if got := p.Update([]int{0, 2}, at(2000)); !slices.Equal(got, []int{0, 2}) {
		t.Errorf("peaks fall to the bar, not below: got %v", got)
	}
}
