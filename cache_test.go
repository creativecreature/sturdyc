package sturdyc_test

import (
	"testing"
	"time"

	"github.com/creativecreature/sturdyc"
)

type distributionTestCase struct {
	name                string
	capacity            int
	numShards           int
	tolerancePercentage int
	keyLength           int
}

func TestShardDistribution(t *testing.T) {
	t.Parallel()

	testCases := []distributionTestCase{
		{
			name:                "1_000_000 capacity, 100 shards, 12% tolerance, 16 key length",
			capacity:            1_000_000,
			numShards:           100,
			tolerancePercentage: 12,
			keyLength:           16,
		},
		{
			name:                "1000 capacity, 2 shards, 12% tolerance, 14 key length",
			capacity:            1000,
			numShards:           2,
			tolerancePercentage: 12,
			keyLength:           14,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := newTestMetricsRecorder(tc.numShards)
			c := sturdyc.New[string](tc.capacity, tc.numShards, time.Hour, 5, sturdyc.WithMetrics(recorder))
			for i := 0; i < tc.capacity; i++ {
				key := randKey(tc.keyLength)
				c.Set(key, "value")
			}
			recorder.validateShardDistribution(t, tc.tolerancePercentage)
		})
	}
}

func TestTimeBasedEviction(t *testing.T) {
	t.Parallel()
	capacity := 10_000
	numShards := 100
	ttl := time.Hour
	evictionPercentage := 5
	evictionInterval := time.Second
	clock := sturdyc.NewTestClock(time.Now())
	metricRecorder := newTestMetricsRecorder(numShards)
	c := sturdyc.New[string](
		capacity,
		numShards,
		ttl,
		evictionPercentage,
		sturdyc.WithMetrics(metricRecorder),
		sturdyc.WithClock(clock),
		sturdyc.WithEvictionInterval(evictionInterval),
	)

	for i := 0; i < capacity; i++ {
		c.Set(randKey(12), "value")
	}

	// Expire all entries.
	clock.Add(ttl + 1)

	// Next, we'll loop through each shard while moving the clock by the evictionInterval. We'll
	// sleep for a brief duration to allow the goroutines that were waiting for the timer to run.
	for i := 0; i < numShards; i++ {
		clock.Add(time.Second + 1)
		time.Sleep(5 * time.Millisecond)
	}

	metricRecorder.Lock()
	defer metricRecorder.Unlock()
	if metricRecorder.evictedEntries != capacity {
		t.Errorf("expected %d evicted entries, got %d", capacity, metricRecorder.evictedEntries)
	}
}

type forcedEvictionTestCase struct {
	name               string
	capacity           int
	writes             int
	numShards          int
	evictionPercentage int
	minEvictions       int
	maxEvictions       int
}

func TestForcedEvictions(t *testing.T) {
	t.Parallel()

	testCases := []forcedEvictionTestCase{
		{
			name:               "1000 capacity, 100_000 writes, 100 shards, 5% forced evictions",
			capacity:           10_000,
			writes:             100_000,
			numShards:          100,
			evictionPercentage: 5,
			minEvictions:       20_000, // Perfect shard distribution.
			maxEvictions:       20_800, // Accounting for a 4% tolerance.
		},
		{
			name:               "100 capacity, 10_000 writes, 10 shards, 1% forced evictions",
			capacity:           100,
			writes:             10_000,
			numShards:          10,
			evictionPercentage: 1,
			minEvictions:       9999,
			maxEvictions:       10001,
		},
		{
			name:               "100 capacity, 1000 writes, 10 shards, 100% forced evictions",
			capacity:           100,
			writes:             1000,
			numShards:          10,
			evictionPercentage: 100,
			minEvictions:       100,
			maxEvictions:       120,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := newTestMetricsRecorder(tc.numShards)
			c := sturdyc.New[string](tc.capacity,
				tc.numShards,
				time.Hour,
				tc.evictionPercentage,
				sturdyc.WithMetrics(recorder),
			)

			// Start by filling the sturdyc.
			for i := 0; i < tc.capacity; i++ {
				key := randKey(12)
				c.Set(key, "value")
			}

			// Next, we'll write to the cache to force evictions.
			for i := 0; i < tc.writes; i++ {
				key := randKey(12)
				c.Set(key, "value")
			}

			if recorder.forcedEvictions < tc.minEvictions || recorder.forcedEvictions > tc.maxEvictions {
				t.Errorf(
					"expected forced evictions between %d and %d, got %d",
					tc.minEvictions, tc.maxEvictions, recorder.forcedEvictions,
				)
			}
		})
	}
}

func TestDisablingForcedEvictionMakesSetANoop(t *testing.T) {
	t.Parallel()

	capacity := 100
	numShards := 10
	ttl := time.Hour
	// Setting the eviction percentage to 0 should disable forced evictions.
	evictionpercentage := 0
	metricRecorder := newTestMetricsRecorder(numShards)
	c := sturdyc.New[string](
		capacity,
		numShards,
		ttl,
		evictionpercentage,
		sturdyc.WithMetrics(metricRecorder),
	)

	for i := 0; i < capacity*10; i++ {
		c.Set(randKey(12), "value")
	}

	metricRecorder.Lock()
	defer metricRecorder.Unlock()
	if metricRecorder.forcedEvictions > 0 {
		t.Errorf("expected no forced evictions, got %d", metricRecorder.forcedEvictions)
	}
}
