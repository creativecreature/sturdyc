package main

import (
	"context"
	"log"
	"time"

	"github.com/creativecreature/sturdyc"
)

type API struct {
	cacheClient *sturdyc.Client
}

func NewAPI(c *sturdyc.Client) *API {
	return &API{c}
}

func (a *API) Get(ctx context.Context, key string) (string, error) {
	// This could be a call to a rate limited service, a database query, etc.
	fetchFn := func(_ context.Context) (string, error) {
		log.Printf("Fetching value for key: %s\n", key)
		return "value", nil
	}
	return sturdyc.GetFetch(ctx, a.cacheClient, key, fetchFn)
}

func main() {
	// ===========================================================
	// ===================== Basic configuration =================
	// ===========================================================
	// Maximum number of entries in the sturdyc.
	capacity := 10000
	// Number of shards to use for the sturdyc.
	numShards := 10
	// Time-to-live for cache entries.
	ttl := 2 * time.Hour
	// Percentage of entries to evict when the cache is full. Setting this
	// to 0 will make set a no-op if the cache has reached its capacity.
	evictionPercentage := 10

	// ===========================================================
	// =================== Stampede protection ===================
	// ===========================================================
	// Set a minimum and maximum refresh delay for the sturdyc. This is
	// used to spread out the refreshes for entries evenly over time.
	minRefreshDelay := time.Millisecond * 10
	maxRefreshDelay := time.Millisecond * 30
	// The base for exponential backoff when retrying a refresh.
	retryBaseDelay := time.Millisecond * 10
	// NOTE: Ignore this for now, it will be shown in the next example.
	storeMisses := true

	// Create a cache client with the specified configuration.
	cacheClient := sturdyc.New(capacity, numShards, ttl, evictionPercentage,
		sturdyc.WithStampedeProtection(minRefreshDelay, maxRefreshDelay, retryBaseDelay, storeMisses),
	)

	// Create a new API instance with the cache client.
	api := NewAPI(cacheClient)

	// We are going to retrieve the values every 10 milliseconds, however the
	// logs will reveal that actual refreshes fluctuate randomly within a 10-30
	// millisecond range. Even if this loop is executed across multiple
	// goroutines, the API call frequency will maintain this variability,
	// ensuring we avoid overloading the API with requests.
	for i := 0; i < 100; i++ {
		val, _ := api.Get(context.Background(), "key")
		log.Printf("Value: %s\n", val)
		time.Sleep(minRefreshDelay)
	}
}
