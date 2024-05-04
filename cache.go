package sturdyc

import (
	"context"
	"sync"
	"time"

	"github.com/cespare/xxhash"
)

type MetricsRecorder interface {
	CacheHit()
	CacheMiss()
	Eviction()
	ForcedEviction()
	EntriesEvicted(int)
	ShardIndex(int)
	CacheBatchRefreshSize(size int)
	ObserveCacheSize(callback func() int)
}

// FetchFn Fetch represents a function that can be used to fetch a single record from a data source.
type FetchFn[T any] func(ctx context.Context) (T, error)

// BatchFetchFn represents a function that can be used to fetch multiple records from a data source.
type BatchFetchFn[T any] func(ctx context.Context, ids []string) (map[string]T, error)

type BatchResponse[T any] map[string]T

// KeyFn is called invoked for each record that a batch fetch
// operation returns. It is used to create unique cache keys.
type KeyFn func(id string) string

// Client holds the cache configuration.
type Client struct {
	ttl              time.Duration
	shards           []*shard
	nextShard        int
	evictionInterval time.Duration
	clock            Clock
	metricsRecorder  MetricsRecorder

	refreshesEnabled bool
	minRefreshTime   time.Duration
	maxRefreshTime   time.Duration
	retryBaseDelay   time.Duration
	storeMisses      bool

	bufferRefreshes       bool
	batchMutex            sync.Mutex
	batchSize             int
	bufferTimeout         time.Duration
	bufferPermutationIDs  map[string][]string
	bufferPermutationChan map[string]chan<- []string

	passthroughPercentage int
	passthroughBuffering  bool

	useRelativeTimeKeyFormat bool
	keyTruncation            time.Duration
}

// New creates a new Client instance with the specified configuration.
//
// `capacity` defines the maximum number of entries that the cache can store.
// `numShards` Is used to set the number of shards. Has to be greater than 0.
// `ttl` Sets the time to live for each entry in the cache. Has to be greater than 0.
// `evictionPercentage` Percentage of items to evict when the cache exceeds its capacity.
// `opts` allows for additional configurations to be applied to the cache client.
func New(capacity, numShards int, ttl time.Duration, evictionPercentage int, opts ...Option) *Client {
	validateArgs(capacity, numShards, ttl, evictionPercentage)

	// Create a new client, and apply the options.
	//nolint: exhaustruct // The options are going to set the remaining fields.
	client := &Client{
		ttl:                   ttl,
		clock:                 NewClock(),
		evictionInterval:      ttl / time.Duration(numShards),
		passthroughPercentage: 100,
	}

	for _, opt := range opts {
		opt(client)
	}

	// We create the shards after we've applied the options to ensure that the correct values are used.
	shardSize := capacity / numShards
	shards := make([]*shard, numShards)
	for i := 0; i < numShards; i++ {
		shard := newShard(
			shardSize,
			ttl,
			evictionPercentage,
			client.clock,
			client.metricsRecorder,
			client.refreshesEnabled,
			client.minRefreshTime,
			client.maxRefreshTime,
			client.retryBaseDelay,
		)
		shards[i] = shard
	}
	client.shards = shards
	client.nextShard = 0

	// Run evictions in a separate goroutine.
	client.startEvictions()

	return client
}

// Size returns the number of entries in the cache.
func (c *Client) Size() int {
	var sum int
	for _, shard := range c.shards {
		sum += shard.size()
	}
	return sum
}

// Delete removes a single entry from the cache.
func (c *Client) Delete(key string) {
	shard := c.getShard(key)
	shard.delete(key)
}

// startEvictions is going to be running in a separate goroutine that we're going to prevent from ever exiting.
func (c *Client) startEvictions() {
	go func() {
		ticker, stop := c.clock.NewTicker(c.evictionInterval)
		defer stop()
		for range ticker {
			if c.metricsRecorder != nil {
				c.metricsRecorder.Eviction()
			}
			c.shards[c.nextShard].evictExpired()
			c.nextShard = (c.nextShard + 1) % len(c.shards)
		}
	}()
}

// getShard returns the shard that should be used for the specified key.
func (c *Client) getShard(key string) *shard {
	hash := xxhash.Sum64String(key)
	shardIndex := hash % uint64(len(c.shards))
	if c.metricsRecorder != nil {
		c.metricsRecorder.ShardIndex(int(shardIndex))
	}
	return c.shards[shardIndex]
}

// reportCacheHits is used to report cache hits and misses to the metrics recorder.
func (c *Client) reportCacheHits(cacheHit bool) {
	if c.metricsRecorder == nil {
		return
	}
	if !cacheHit {
		c.metricsRecorder.CacheMiss()
		return
	}
	c.metricsRecorder.CacheHit()
}

// set writes a single value to the cache. Returns true if it triggered an eviction.
func (c *Client) set(key string, value any, isMissingRecord bool) bool {
	shard := c.getShard(key)
	return shard.set(key, value, isMissingRecord)
}
