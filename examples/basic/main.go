package main

import (
	"log"
	"time"

	"github.com/creativecreature/sturdyc"
)

func main() {
	// Maximum number of entries in the sturdyc.
	capacity := 10000
	// Number of shards to use for the sturdyc.
	numShards := 10
	// Time-to-live for cache entries.
	ttl := 2 * time.Hour
	// Percentage of entries to evict when the cache is full. Setting this
	// to 0 will make set a no-op if the cache has reached its capacity.
	evictionPercentage := 10

	// Create a cache client with the specified configuration.
	cacheClient := sturdyc.New[any](capacity, numShards, ttl, evictionPercentage)

	// We can then use the client with generic functions to store and retrieve values.
	cacheClient.Set("key1", "value")
	if val, ok := sturdyc.Get[any, string](cacheClient, "key1"); ok {
		log.Println(val)
	}

	cacheClient.Set("key2", 1)
	if val, ok := sturdyc.Get[any, int](cacheClient, "key2"); ok {
		log.Println(val)
	}
}
