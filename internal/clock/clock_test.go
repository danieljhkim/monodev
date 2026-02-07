package clock

import (
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	clock := &RealClock{}

	t.Run("returns current time", func(t *testing.T) {
		before := time.Now()
		actual := clock.Now()
		after := time.Now()

		// Verify the returned time is between before and after
		if actual.Before(before) || actual.After(after) {
			t.Errorf("RealClock.Now() returned time outside expected range: got %v, expected between %v and %v", actual, before, after)
		}
	})

	t.Run("subsequent calls return increasing times", func(t *testing.T) {
		first := clock.Now()
		time.Sleep(1 * time.Millisecond)
		second := clock.Now()

		if !second.After(first) {
			t.Errorf("Expected second call to return later time: first=%v, second=%v", first, second)
		}
	})
}

func TestFakeClock_Now(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := NewFakeClock(fixedTime)

	t.Run("returns fixed time", func(t *testing.T) {
		actual := clock.Now()
		if !actual.Equal(fixedTime) {
			t.Errorf("FakeClock.Now() = %v, want %v", actual, fixedTime)
		}
	})

	t.Run("subsequent calls return same time", func(t *testing.T) {
		first := clock.Now()
		time.Sleep(1 * time.Millisecond)
		second := clock.Now()

		if !first.Equal(second) {
			t.Errorf("FakeClock.Now() should return consistent time: first=%v, second=%v", first, second)
		}
	})
}

func TestFakeClock_Set(t *testing.T) {
	initialTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewFakeClock(initialTime)

	t.Run("updates the current time", func(t *testing.T) {
		newTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
		clock.Set(newTime)

		actual := clock.Now()
		if !actual.Equal(newTime) {
			t.Errorf("After Set(), Now() = %v, want %v", actual, newTime)
		}
	})

	t.Run("can set time backwards", func(t *testing.T) {
		futureTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		clock.Set(futureTime)

		pastTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		clock.Set(pastTime)

		actual := clock.Now()
		if !actual.Equal(pastTime) {
			t.Errorf("After setting time backwards, Now() = %v, want %v", actual, pastTime)
		}
	})
}

func TestFakeClock_Advance(t *testing.T) {
	initialTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewFakeClock(initialTime)

	t.Run("advances time by duration", func(t *testing.T) {
		duration := 2 * time.Hour
		expectedTime := initialTime.Add(duration)

		clock.Advance(duration)
		actual := clock.Now()

		if !actual.Equal(expectedTime) {
			t.Errorf("After Advance(%v), Now() = %v, want %v", duration, actual, expectedTime)
		}
	})

	t.Run("multiple advances accumulate", func(t *testing.T) {
		clock.Set(initialTime)

		clock.Advance(1 * time.Hour)
		clock.Advance(30 * time.Minute)
		clock.Advance(15 * time.Second)

		expectedTime := initialTime.Add(1*time.Hour + 30*time.Minute + 15*time.Second)
		actual := clock.Now()

		if !actual.Equal(expectedTime) {
			t.Errorf("After multiple advances, Now() = %v, want %v", actual, expectedTime)
		}
	})

	t.Run("can advance by negative duration", func(t *testing.T) {
		clock.Set(initialTime)

		clock.Advance(-1 * time.Hour)
		expectedTime := initialTime.Add(-1 * time.Hour)
		actual := clock.Now()

		if !actual.Equal(expectedTime) {
			t.Errorf("After negative advance, Now() = %v, want %v", actual, expectedTime)
		}
	})

	t.Run("advance by zero has no effect", func(t *testing.T) {
		clock.Set(initialTime)

		clock.Advance(0)
		actual := clock.Now()

		if !actual.Equal(initialTime) {
			t.Errorf("After advancing by zero, Now() = %v, want %v", actual, initialTime)
		}
	})
}

func TestNewFakeClock(t *testing.T) {
	t.Run("creates clock with specified time", func(t *testing.T) {
		targetTime := time.Date(2024, 3, 15, 9, 30, 45, 0, time.UTC)
		clock := NewFakeClock(targetTime)

		actual := clock.Now()
		if !actual.Equal(targetTime) {
			t.Errorf("NewFakeClock created clock with time %v, want %v", actual, targetTime)
		}
	})

	t.Run("creates independent clocks", func(t *testing.T) {
		time1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		time2 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

		clock1 := NewFakeClock(time1)
		clock2 := NewFakeClock(time2)

		if clock1.Now().Equal(clock2.Now()) {
			t.Error("Independent FakeClocks should have independent times")
		}

		clock1.Advance(1 * time.Hour)
		if clock1.Now().Equal(clock2.Now()) {
			t.Error("Advancing one clock should not affect another")
		}
	})
}
