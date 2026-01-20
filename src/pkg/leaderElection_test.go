package pkg

import (
	"sync"
	"testing"

	"github.com/rocktavious/autopilot/v2023"
)

func TestSetLeaderGetLeader(t *testing.T) {
	// Arrange - reset to known state
	setLeader(false)

	// Act & Assert - basic functionality
	autopilot.Equals(t, false, getLeader())

	setLeader(true)
	autopilot.Equals(t, true, getLeader())

	setLeader(false)
	autopilot.Equals(t, false, getLeader())
}

func TestSetLeaderGetLeader_ConcurrentAccess(t *testing.T) {
	// This test verifies that concurrent access to setLeader/getLeader
	// does not cause data races. Run with -race flag to detect races.
	// Arrange
	setLeader(false)
	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // writers and readers

	// Act - concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				setLeader(id%2 == 0)
			}
		}(i)
	}

	// Act - concurrent readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = getLeader()
			}
		}()
	}

	wg.Wait()

	// Assert - if we get here without race detector complaints, the test passes
	// Final state is indeterminate due to concurrent writes, but no panics or races
	_ = getLeader() // should not panic
}

func TestSetLeaderGetLeader_ConcurrentReadWrite(t *testing.T) {
	// Specifically test the pattern used in leaderElection.go callbacks
	// where OnNewLeader reads while OnStartedLeading/OnStoppedLeading write
	setLeader(false)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Simulate OnStartedLeading/OnStoppedLeading callbacks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				setLeader(true)
				setLeader(false)
			}
		}
	}()

	// Simulate OnNewLeader callback reading isLeader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			// Simulate the condition checks in OnNewLeader
			_ = getLeader()
		}
	}()

	close(done)
	wg.Wait()
}
