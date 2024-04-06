package main

import (
	"context"
	"log"
	"time"

	"github.com/creativecreature/sturdyc"
)

type OrderAPI struct {
	cacheClient *sturdyc.Client
}

func NewOrderAPI() *OrderAPI {
	// Maximum number of entries in the sturdyc.
	capacity := 10000
	// Number of shards to use for the sturdyc.
	numShards := 10
	// Time-to-live for cache entries.
	ttl := 2 * time.Hour
	// Percentage of entries to evict when the cache is full. Setting this
	// to 0 will make set a no-op if the cache has reached its capacity.
	evictionPercentage := 10
	// Set a minimum and maximum refresh delay for the sturdyc. This is
	// used to spread out the refreshes for entries evenly over time.
	minRefreshDelay := time.Second
	maxRefreshDelay := time.Second * 2
	// The base for exponential backoff when retrying a refresh.
	retryBaseDelay := time.Millisecond * 10
	// Whether to store misses in the sturdyc. This can be useful to
	// prevent the cache from fetching a non-existent key repeatedly.
	storeMisses := true
	// With refresh buffering enabled, the cache will buffer refreshes
	// until the batch size is reached or the buffer timeout is hit.
	batchSize := 5
	batchBufferTimeout := time.Second * 30

	// Create a new cache client with the specified configuration.
	cacheClient := sturdyc.New(capacity, numShards, ttl, evictionPercentage,
		sturdyc.WithStampedeProtection(minRefreshDelay, maxRefreshDelay, retryBaseDelay, storeMisses),
		sturdyc.WithRefreshBuffering(batchSize, batchBufferTimeout),
	)

	return &OrderAPI{
		cacheClient: cacheClient,
	}
}

func (a *OrderAPI) OrderStatus(ctx context.Context, ids []string, opts OrderOptions) (map[string]string, error) {
	// We use the  PermutedBatchKeyFn when an ID isn't enough to uniquely identify a
	// record. The cache is going to store each id once per set of options. In a more
	// realistic scenario, the opts would be query params or arguments to a query.
	cacheKeyFn := a.cacheClient.PermutatedBatchKeyFn("key", opts)

	// We'll create a fetchFn with a closure that captures the options. For this
	// simple example, it logs and returns the status for each order, but you could
	// just as easily have called an external API.
	fetchFn := func(_ context.Context, ids []string) (map[string]string, error) {
		log.Printf("IDs: %v, carrier: %s, latest delivery time: %s\n", ids, opts.CarrierName, opts.LatestDeliveryTime)
		response := map[string]string{}
		for _, id := range ids {
			response[id] = "Available"
		}
		return response, nil
	}
	return sturdyc.GetFetchBatch(ctx, a.cacheClient, ids, cacheKeyFn, fetchFn)
}
