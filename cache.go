package sturdyc

import (
	"context"
	"errors"
	"hash/fnv"
	"maps"
	"sync"
	"time"
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
		ttl:              ttl,
		clock:            NewClock(),
		evictionInterval: ttl / time.Duration(numShards),
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
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(key))
	hash := hasher.Sum64()
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

// get retrieves a value from the cache and performs a type assertion to the desired type.
func get[T any](c *Client, key string) (value T, exists, ignore, refresh bool) {
	shard := c.getShard(key)
	entry, exists, ignore, refresh := shard.get(key)
	c.reportCacheHits(exists)

	if !exists {
		return value, false, false, false
	}

	val, ok := entry.(T)
	if !ok {
		return value, false, false, false
	}

	return val, exists, ignore, refresh
}

// Get retrieves a value from the cache and performs a type assertion to the desired type.
func Get[T any](c *Client, key string) (T, bool) {
	value, ok, _, _ := get[T](c, key)
	return value, ok
}

// GetFetch attempts to retrieve the specified key from the cache. If the value
// is absent, it invokes the "fetchFn" to obtain it, and subsequently stores
// it. Additionally, when stampede protection is active, GetFetch assesses
// whether the record requires refreshing and, if so, schedules this task to be
// executed in the background.

// GetFetch attempts to retrieve the specified key from the cache. If the value
// is absent, it invokes the "fetchFn" function to obtain it and then stores
// the result. Additionally, when stampede protection is enabled, GetFetch
// determines if the record needs refreshing and, if necessary, schedules this
// task for background execution.
func GetFetch[T any](ctx context.Context, c *Client, key string, fetchFn FetchFn[T]) (T, error) {
	// Begin by checking if we have the item in our cache.
	value, ok, shouldIgnore, shouldRefresh := get[T](c, key)

	// We have the item cached, and we'll check if it should be refreshed in the background.
	if shouldRefresh {
		safeGo(func() {
			refresh(c, key, fetchFn)
		})
	}

	if shouldIgnore {
		return value, ErrMissingRecord
	}

	// If we don't have this item in our cache, we'll fetch it
	if !ok {
		response, err := fetchFn(ctx)
		if err != nil {
			// In case of an error, we'll only cache the response if the fetchFn returned an ErrMissingRecord.
			if c.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
				c.set(key, response, true)
			}
			return response, err
		}

		// Cache the response
		c.set(key, response, false)
		return response, err
	}

	return value, nil
}

// GetFetchBatch attempts to retrieve the specified ids from the cache. If any
// of the values are absent, it invokes the fetchFn function to obtain them and
// then stores the result. Additionally, when stampede protection is enabled,
// GetFetch determines if any of the records needs refreshing and, if
// necessary, schedules this to be performed in the background.
func GetFetchBatch[T any](
	ctx context.Context,
	c *Client,
	ids []string,
	keyFn KeyFn,
	fetchFn BatchFetchFn[T],
) (map[string]T, error) {
	cachedRecords := make(map[string]T)
	cacheMisses := make([]string, 0)
	idsToRefresh := make([]string, 0)

	// Group the IDs into cached records, cache misses, and ids which are due for a refresh.
	for _, id := range ids {
		key := keyFn(id)
		value, exists, shouldIgnore, shouldRefresh := get[T](c, key)

		// Check if the record should be refreshed in the background.
		if shouldRefresh {
			idsToRefresh = append(idsToRefresh, id)
		}

		if shouldIgnore {
			continue
		}

		if !exists {
			cacheMisses = append(cacheMisses, id)
			continue
		}

		cachedRecords[id] = value
	}

	// If any records need to be refreshed, we'll do so in the background.
	if len(idsToRefresh) > 0 {
		if c.bufferRefreshes {
			safeGo(func() {
				bufferBatchRefresh(c, idsToRefresh, keyFn, fetchFn)
			})
		} else {
			safeGo(func() {
				refreshBatch(c, idsToRefresh, keyFn, fetchFn)
			})
		}
	}

	// If we were able to retrieve all records from the cache, we can return them straight away.
	if len(cacheMisses) == 0 {
		return cachedRecords, nil
	}

	// Fetch the missing records.
	response, err := fetchFn(ctx, cacheMisses)
	if err != nil {
		// We had some records in the cache, but the remaining records couldn't be
		// retrieved. Therefore, we'll return the cached records along with a
		// ErrOnlyCachedRecords error, and let the caller decide what to do.
		if len(cachedRecords) > 0 {
			return cachedRecords, ErrOnlyCachedRecords
		}
		return cachedRecords, err
	}

	// Check if we should store any of these IDs as a missing record.
	if c.storeMisses && len(response) < len(cacheMisses) {
		for _, id := range cacheMisses {
			if v, ok := response[id]; !ok {
				c.set(keyFn(id), v, true)
			}
		}
	}

	// Cache the fetched records, and merge them with the ones we already had.
	for id, record := range response {
		c.set(keyFn(id), record, false)
	}
	maps.Copy(cachedRecords, response)

	return cachedRecords, nil
}

// Set writes a single value to the cache. Returns true if it triggered an eviction.
func Set(c *Client, key string, value any) bool {
	return c.set(key, value, false)
}

// SetMany writes multiple values to the cache. Returns true if it triggered an eviction.
func SetMany[T any](c *Client, records map[string]T, cacheKeyFn KeyFn) bool {
	var triggeredEviction bool
	for id, value := range records {
		evicted := c.set(cacheKeyFn(id), value, false)
		if evicted {
			triggeredEviction = true
		}
	}
	return triggeredEviction
}
