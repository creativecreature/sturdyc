package sturdyc

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

type shard struct {
	capacity           int
	ttl                time.Duration
	mu                 sync.RWMutex
	entries            map[string]*entry
	clock              Clock
	metricsRecorder    MetricsRecorder
	evictionPercentage int

	refreshesEnabled bool
	minRefreshTime   time.Duration
	maxRefreshTime   time.Duration
	retryBaseDelay   time.Duration
}

func newShard(
	capacity int,
	ttl time.Duration,
	evictionPercentage int,
	clock Clock,
	metricsRecorder MetricsRecorder,
	refreshesEnabled bool,
	minRefreshTime,
	maxRefreshTime time.Duration,
	retryBaseDelay time.Duration,
) *shard {
	return &shard{
		capacity:           capacity,
		ttl:                ttl,
		mu:                 sync.RWMutex{},
		entries:            make(map[string]*entry),
		evictionPercentage: evictionPercentage,
		clock:              clock,
		metricsRecorder:    metricsRecorder,
		minRefreshTime:     minRefreshTime,
		maxRefreshTime:     maxRefreshTime,
		refreshesEnabled:   refreshesEnabled,
		retryBaseDelay:     retryBaseDelay,
	}
}

func (s *shard) size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// evictExpired evicts all the expired entries in the shard.
func (s *shard) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	var entriesEvicted int
	for _, e := range s.entries {
		if s.clock.Now().After(e.expiresAt) {
			delete(s.entries, e.key)
			entriesEvicted++
		}
	}
	if s.metricsRecorder != nil && entriesEvicted > 0 {
		s.metricsRecorder.EntriesEvicted(entriesEvicted)
	}
}

// forceEvict evicts a certain percentage of the entries in the shard
// based on the expiration time. NOTE: Should be called with a lock.
func (s *shard) forceEvict() {
	if s.metricsRecorder != nil {
		s.metricsRecorder.ForcedEviction()
	}

	expirationTimes := make([]time.Time, 0, len(s.entries))
	for _, e := range s.entries {
		expirationTimes = append(expirationTimes, e.expiresAt)
	}

	cutoff := FindCutoff(expirationTimes, float64(s.evictionPercentage)/100)
	var entriesEvicted int
	for key, e := range s.entries {
		if e.expiresAt.Before(cutoff) {
			delete(s.entries, key)
			entriesEvicted++
		}
	}
	if s.metricsRecorder != nil && entriesEvicted > 0 {
		s.metricsRecorder.EntriesEvicted(entriesEvicted)
	}
}

func (s *shard) get(key string) (val any, exists, ignore, refresh bool) {
	s.mu.RLock()
	if item, ok := s.entries[key]; ok {
		if s.clock.Now().After(item.expiresAt) {
			s.mu.RUnlock()
			return nil, false, false, false
		}

		shouldRefresh := s.refreshesEnabled && s.clock.Now().After(item.refreshAt)
		s.mu.RUnlock()
		if shouldRefresh {
			// During the time it takes to switch to a write lock, another goroutine
			// might have acquired it and moved the refreshAt before we could.
			s.mu.Lock()
			shoulStillRefresh := s.clock.Now().After(item.refreshAt)
			if !shoulStillRefresh {
				s.mu.Unlock()
				return item.value, true, item.isMissingRecord, false
			}

			// Update the "refreshAt" so no other goroutines attempts to refresh the same entry.
			nextRefresh := math.Pow(2, float64(item.numOfRefreshRetries)) * float64(s.retryBaseDelay)
			item.refreshAt = s.clock.Now().Add(time.Duration(nextRefresh))
			item.numOfRefreshRetries++
			s.mu.Unlock()
		}
		return item.value, true, item.isMissingRecord, shouldRefresh
	}
	s.mu.RUnlock()
	return nil, false, false, false
}

// set sets a key-value pair in the shard. Returns true if it triggered an eviction.
func (s *shard) set(key string, value any, isMissingRecord bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()

	// Check we need to perform an eviction first.
	evict := len(s.entries) >= s.capacity

	// If the cache is configured to not evict any entries, return early.
	if s.evictionPercentage < 1 {
		return false
	}

	if evict {
		s.forceEvict()
	}

	//nolint: exhaustruct // we are going to set the remaining fields based on config.
	e := &entry{
		key:             key,
		value:           value,
		expiresAt:       now.Add(s.ttl),
		isMissingRecord: isMissingRecord,
	}

	if s.refreshesEnabled {
		// Add a random padding to the refresh times in order to spread them out more evenly.
		padding := time.Duration(rand.Int64N(int64(s.maxRefreshTime - s.minRefreshTime)))
		e.refreshAt = now.Add(s.minRefreshTime + padding)
		e.numOfRefreshRetries = 0
	}
	s.entries[key] = e

	return evict
}
