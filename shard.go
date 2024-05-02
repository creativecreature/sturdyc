package sturdyc

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

type shard struct {
	sync.RWMutex
	capacity           int
	ttl                time.Duration
	entries            map[string]*entry
	clock              Clock
	metricsRecorder    MetricsRecorder
	evictionPercentage int
	refreshesEnabled   bool
	minRefreshTime     time.Duration
	maxRefreshTime     time.Duration
	retryBaseDelay     time.Duration
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
	s.RLock()
	defer s.RUnlock()
	return len(s.entries)
}

// evictExpired evicts all the expired entries in the shard.
func (s *shard) evictExpired() {
	s.Lock()
	defer s.Unlock()

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
	entriesEvicted := 0
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
	s.RLock()
	item, ok := s.entries[key]
	if !ok {
		s.RUnlock()
		return nil, false, false, false
	}

	if s.clock.Now().After(item.expiresAt) {
		s.RUnlock()
		return nil, false, false, false
	}

	shouldRefresh := s.refreshesEnabled && s.clock.Now().After(item.refreshAt)
	if shouldRefresh {
		// Release the read lock, and switch to a write lock.
		s.RUnlock()
		s.Lock()

		// However, during the time it takes to switch locks, another goroutine
		// might have acquired it and moved the refreshAt. Therefore, we'll have to
		// check if this operation should still be performed.
		if !s.clock.Now().After(item.refreshAt) {
			s.Unlock()
			return item.value, true, item.isMissingRecord, false
		}

		// Update the "refreshAt" so no other goroutines attempts to refresh the same entry.
		nextRefresh := math.Pow(2, float64(item.numOfRefreshRetries)) * float64(s.retryBaseDelay)
		item.refreshAt = s.clock.Now().Add(time.Duration(nextRefresh))
		item.numOfRefreshRetries++

		s.Unlock()
		return item.value, true, item.isMissingRecord, shouldRefresh
	}

	s.RUnlock()
	return item.value, true, item.isMissingRecord, false
}

// set sets a key-value pair in the shard. Returns true if it triggered an eviction.
func (s *shard) set(key string, value any, isMissingRecord bool) bool {
	s.Lock()
	defer s.Unlock()

	// Check we need to perform an eviction first.
	evict := len(s.entries) >= s.capacity

	// If the cache is configured to not evict any entries,
	// and we're att full capacity, we'll return early.
	if s.evictionPercentage < 1 && evict {
		return false
	}

	if evict {
		s.forceEvict()
	}

	now := s.clock.Now()
	newEntry := &entry{
		key:             key,
		value:           value,
		expiresAt:       now.Add(s.ttl),
		isMissingRecord: isMissingRecord,
	}

	if s.refreshesEnabled {
		// If there is a difference between the min- and maxRefreshTime we'll use that to
		// set a random padding so that the refreshes get spread out evenly over time.
		var padding time.Duration
		if s.minRefreshTime != s.maxRefreshTime {
			padding = time.Duration(rand.Int64N(int64(s.maxRefreshTime - s.minRefreshTime)))
		}
		newEntry.refreshAt = now.Add(s.minRefreshTime + padding)
		newEntry.numOfRefreshRetries = 0
	}

	s.entries[key] = newEntry
	return evict
}

func (s *shard) delete(key string) {
	s.Lock()
	defer s.Unlock()
	delete(s.entries, key)
}
