package sequencer

import (
	"sync/atomic"
)

var (
	// isPaused is the global sequencer paused state
	// 0 = active, 1 = paused
	isPaused int32
)

// IsPaused returns true if the sequencer is currently paused
func IsPaused() bool {
	return atomic.LoadInt32(&isPaused) == 1
}

// SetPaused sets the sequencer paused state
// This function is used by Apollo configuration to update the sequencer state
func SetPaused(paused bool) {
	if paused {
		atomic.StoreInt32(&isPaused, 1)
	} else {
		atomic.StoreInt32(&isPaused, 0)
	}
}
