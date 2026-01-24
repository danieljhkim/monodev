package clock

import "time"

// Clock provides an abstraction for time operations to enable deterministic testing.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// RealClock implements Clock using the system time.
type RealClock struct{}

// Now returns the current system time.
func (c *RealClock) Now() time.Time {
	return time.Now()
}

// FakeClock implements Clock with a fixed time for testing.
type FakeClock struct {
	current time.Time
}

// NewFakeClock creates a new FakeClock with the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{current: t}
}

// Now returns the fixed time.
func (c *FakeClock) Now() time.Time {
	return c.current
}

// Set updates the fixed time.
func (c *FakeClock) Set(t time.Time) {
	c.current = t
}

// Advance moves the fixed time forward by the given duration.
func (c *FakeClock) Advance(d time.Duration) {
	c.current = c.current.Add(d)
}
