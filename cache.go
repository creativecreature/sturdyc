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

type KeyFn func(string) string

type FetchFn[T any] func(ctx context.Context) (T, error)

type BatchFetchFn[T any] func(ctx context.Context, ids []string) (map[string]T, error)

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

	bufferMutex           sync.Mutex
	bufferRefreshes       bool
	maxBufferSize         int
	bufferTimeout         time.Duration
	bufferIdentifierIDs   map[string][]string
	bufferIdentifierChans map[string]chan<- []string

	useRelativeTimeKeyFormat bool
	keyTruncation            time.Duration
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

func (c *Client) set(key string, value any, isMissingRecord bool) bool {
	shard := c.getShard(key)
	return shard.set(key, value, isMissingRecord)
}

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

func GetFetch[T any](ctx context.Context, client *Client, key string, fetchFn FetchFn[T]) (T, error) {
	// Begin by checking if we have the item in our cache.
	value, ok, shouldIgnore, shouldRefresh := get[T](client, key)

	// We have the item cached and we'll check if it should be refreshed in the background.
	if shouldRefresh {
		safeGo(func() {
			refresh(client, key, fetchFn)
		})
	}

	if shouldIgnore {
		return value, ErrMissingRecordCooldown
	}

	// If we don't have this item in our cache, we'll fetch it
	if !ok {
		response, err := fetchFn(ctx)
		if err != nil {
			// In case of an error, we'll only cache the response if the fetchFn returned an ErrMissingRecord.
			if client.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
				client.set(key, response, true)
			}
			return response, err
		}

		// Cache the response
		client.set(key, response, false)
		return response, err
	}

	return value, nil
}

func GetFetchBatch[T any](
	ctx context.Context,
	client *Client,
	ids []string,
	keyFn KeyFn,
	fetchFn BatchFetchFn[T],
) (map[string]T, error) {
	cachedRecords := make(map[string]T)
	cacheMisses := make([]string, 0)
	idsToRefresh := make([]string, 0)
	for _, id := range ids {
		key := keyFn(id)
		value, exists, shouldIgnore, shouldRefresh := get[T](client, key)

		// Check if the record should be refreshed in the background.
		if shouldRefresh {
			idsToRefresh = append(idsToRefresh, id)
		}

		// We'll ignore records that are in cooldown.
		if shouldIgnore {
			continue
		}

		if !exists {
			cacheMisses = append(cacheMisses, id)
			continue
		}

		cachedRecords[id] = value
	}

	// Refresh records in the background
	if len(idsToRefresh) > 0 {
		if client.bufferRefreshes {
			safeGo(func() {
				bufferBatchRefresh(client, idsToRefresh, keyFn, fetchFn)
			})
		} else {
			safeGo(func() {
				refreshBatch(client, idsToRefresh, keyFn, fetchFn)
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
		// We had some records in the cache, but the remaining records couldn't be retrieved. Therefore,
		// we'll return a ErrOnlyCachedRecords error, and let the caller decide what to do.
		if len(cachedRecords) > 0 {
			return cachedRecords, ErrOnlyCachedRecords
		}
		return cachedRecords, err
	}

	// Check if we should store any missing records with a cooldown.
	if client.storeMisses && len(response) < len(cacheMisses) {
		for _, id := range cacheMisses {
			if v, ok := response[id]; !ok {
				client.set(keyFn(id), v, true)
			}
		}
	}

	// Cache the fetched records.
	for id, record := range response {
		client.set(keyFn(id), record, false)
	}

	// Merge the cached records with the fetched records.
	maps.Copy(cachedRecords, response)

	return cachedRecords, nil
}

// Set sets a value in the cache. Returns true if it triggered an eviction.
func Set(c *Client, key string, value any) bool {
	return c.set(key, value, false)
}

func SetMany[T any](c *Client, records map[string]T, cacheKeyFn KeyFn) {
	for id, value := range records {
		c.set(cacheKeyFn(id), value, false)
	}
}
