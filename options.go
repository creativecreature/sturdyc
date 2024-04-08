package sturdyc

import "time"

type Option func(*Client)

// WithMetrics is used to make the cache report metrics.
func WithMetrics(recorder MetricsRecorder) Option {
	return func(c *Client) {
		recorder.ObserveCacheSize(c.Size)
		c.metricsRecorder = recorder
	}
}

// WithClock can be used to change the clock that the cache uses. This is useful for testing.
func WithClock(clock Clock) Option {
	return func(c *Client) {
		c.clock = clock
	}
}

// WithEvictionInterval sets the interval at which the cache scans a shard to evict expired entries.
func WithEvictionInterval(interval time.Duration) Option {
	return func(c *Client) {
		c.evictionInterval = interval
	}
}

func WithStampedeProtection(
	minRefreshTime,
	maxRefreshTime,
	retryBaseDelay time.Duration,
	storeMisses bool,
) Option {
	return func(c *Client) {
		c.refreshesEnabled = true
		c.minRefreshTime = minRefreshTime
		c.maxRefreshTime = maxRefreshTime
		c.retryBaseDelay = retryBaseDelay
		c.storeMisses = storeMisses
	}
}

func WithRefreshBuffering(batchSize int, maxBufferTime time.Duration) Option {
	return func(c *Client) {
		c.bufferRefreshes = true
		c.batchSize = batchSize
		c.bufferTimeout = maxBufferTime
		c.bufferPermutationIDs = make(map[string][]string)
		c.bufferPermutationChan = make(map[string]chan<- []string)
	}
}

func WithRelativeTimeKeyFormat(truncation time.Duration) Option {
	return func(c *Client) {
		c.useRelativeTimeKeyFormat = true
		c.keyTruncation = truncation
	}
}

// validateArgs is a helper function that panics if the arguments are invalid.
func validateArgs(capacity, numShards int, ttl time.Duration, evictionPercentage int) {
	if capacity <= 0 {
		panic("capacity must be greater than 0")
	}

	if numShards <= 0 {
		panic("numShards must be greater than 0")
	}

	if numShards > capacity {
		panic("numShards must be less than or equal to capacity")
	}

	if ttl <= 0 {
		panic("ttl must be greater than 0")
	}

	if evictionPercentage < 0 || evictionPercentage > 100 {
		panic("evictionPercentage must be between 0 and 100")
	}
}
