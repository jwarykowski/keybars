package bars

import "time"

// Peaks tracks a falling peak dot per bar, like a hi-fi meter: it jumps to
// the bar's top, holds there, then falls at a steady rate.
type Peaks struct {
	hold  time.Duration
	fall  float64 // levels per second
	level []float64
	since []time.Time // when each peak was last pushed up
	last  time.Time
}

// NewPeaks returns peaks that hold for hold, then fall fall levels a second.
func NewPeaks(hold time.Duration, fall float64) *Peaks {
	return &Peaks{hold: hold, fall: fall}
}

// Update moves the peaks for heights at now and returns each bar's peak
// level (0 = none).
func (p *Peaks) Update(heights []int, now time.Time) []int {
	if len(p.level) != len(heights) {
		p.level = make([]float64, len(heights))
		p.since = make([]time.Time, len(heights))
		p.last = now
	}
	out := make([]int, len(heights))
	for i, h := range heights {
		if float64(h) >= p.level[i] {
			p.level[i], p.since[i] = float64(h), now
		} else if held := p.since[i].Add(p.hold); now.After(held) {
			// fall only for the time since the hold ended
			from := p.last
			if held.After(from) {
				from = held
			}
			p.level[i] = max(float64(h), p.level[i]-p.fall*now.Sub(from).Seconds())
		}
		out[i] = int(p.level[i] + 0.5)
	}
	p.last = now
	return out
}
