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
	cacheClient := sturdyc.New[int](capacity, numShards, ttl, evictionPercentage)

	cacheClient.Set("key1", 99)

	log.Println(cacheClient.Size())
	log.Println(cacheClient.Get("key1"))

	cacheClient.Delete("key1")
	log.Println(cacheClient.Size())
	log.Println(cacheClient.Get("key1"))
}
