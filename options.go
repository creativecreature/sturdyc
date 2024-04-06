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
		c.maxBufferSize = batchSize
		c.bufferTimeout = maxBufferTime
		c.bufferIdentifierIDs = make(map[string][]string)
		c.bufferIdentifierChans = make(map[string]chan<- []string)
	}
}

func WithRelativeTimeKeyFormat(truncation time.Duration) Option {
	return func(c *Client) {
		c.useRelativeTimeKeyFormat = true
		c.keyTruncation = truncation
	}
}
