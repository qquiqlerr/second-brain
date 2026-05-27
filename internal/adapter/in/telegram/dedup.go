// Package telegram contains the driving adapter that bridges Telegram updates
// to the IngestDumpUseCase.
package telegram

import (
	"container/list"
	"sync"
)

// UpdateDedup remembers the most recent N update IDs to suppress duplicates
// produced by Telegram retries or operator-triggered redelivery.
//
// Design: once an update ID is evicted from the active window it is moved to
// a "recently-evicted" set so that a late-arriving duplicate of an old ID
// does not displace a still-live entry from the active window.
type UpdateDedup struct {
	cap     int
	mu      sync.Mutex
	active  map[int64]*list.Element // IDs inside the LRU window
	evicted map[int64]struct{}       // IDs that left the window; not re-inserted
	lru     *list.List
}

// NewUpdateDedup returns a dedup that remembers the last cap update IDs.
func NewUpdateDedup(cap int) *UpdateDedup {
	if cap < 1 {
		cap = 1
	}
	return &UpdateDedup{
		cap:     cap,
		active:  make(map[int64]*list.Element, cap),
		evicted: make(map[int64]struct{}, cap),
		lru:     list.New(),
	}
}

// Seen reports whether updateID was previously seen and records it otherwise.
//
//   - Returns true  → duplicate; caller should discard the update.
//   - Returns false → first sighting; caller should process the update.
//
// An update ID that has been evicted from the active window returns false but
// is NOT re-inserted, preserving the currently active window intact.
func (d *UpdateDedup) Seen(updateID int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Already in the active window → confirmed duplicate.
	if _, ok := d.active[updateID]; ok {
		return true
	}

	// Previously evicted → acknowledge without re-inserting.
	if _, ok := d.evicted[updateID]; ok {
		delete(d.evicted, updateID)
		return false
	}

	// Brand-new ID: insert, evicting the oldest active entry if at capacity.
	if d.lru.Len() == d.cap {
		oldest := d.lru.Back()
		if oldest != nil {
			evictedID := oldest.Value.(int64)
			delete(d.active, evictedID)
			d.lru.Remove(oldest)
			d.evicted[evictedID] = struct{}{}
		}
	}
	d.active[updateID] = d.lru.PushFront(updateID)
	return false
}
