package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/creativecreature/sturdyc"
)

type API struct {
	*sturdyc.Client[string]
	count int
}

func NewAPI(c *sturdyc.Client[string]) *API {
	return &API{c, 0}
}

func (a *API) Get(ctx context.Context, key string) (string, error) {
	fetchFn := func(_ context.Context) (string, error) {
		log.Printf("Fetching value for key: %s\n", key)
		a.count++
		if a.count < 3 {
			return "", sturdyc.ErrStoreMissingRecord
		}
		return "value", nil
	}
	return a.GetFetch(ctx, key, fetchFn)
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
	// Tell the cache to store missing records.
	storeMisses := true

	// Create a cache client with the specified configuration.
	cacheClient := sturdyc.New[string](capacity, numShards, ttl, evictionPercentage,
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
		val, err := api.Get(context.Background(), "key")
		if errors.Is(err, sturdyc.ErrMissingRecord) {
			log.Println("Record does not exist.")
		}
		if err == nil {
			log.Printf("Value: %s\n", val)
		}
		time.Sleep(minRefreshDelay)
	}
}
