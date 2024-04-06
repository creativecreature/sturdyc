# `sturdyc`: a caching library for building sturdy systems
[![Go Reference](https://pkg.go.dev/badge/github.com/creativecreature/sturdyc.svg)](https://pkg.go.dev/github.com/creativecreature/sturdyc)
[![Go Report Card](https://goreportcard.com/badge/github.com/creativecreature/sturdyc)](https://goreportcard.com/report/github.com/creativecreature/sturdyc)
[![codecov](https://codecov.io/gh/creativecreature/sturdyc/graph/badge.svg?token=CYSKW3Z7E6)](https://codecov.io/gh/creativecreature/sturdyc)

`Sturdyc` offers all the features you would expect from a caching library,
along with additional functionality designed to help you build robust
applications, such as:
- **stampede protection**
- **permutated cache keys**
- **request deduplication**
- **refresh buffering**

# Installing
```sh
go get github.com/creativecreature/sturdyc
```

# At a glance
The package exports the following functions:
- Use [`sturdyc.Set`](https://pkg.go.dev/github.com/creativecreature/sturdyc#Set) to write a record to the cache.
- Use [`sturdyc.Get`](https://pkg.go.dev/github.com/creativecreature/sturdyc#Get) to retrieve a record from the cache.
- Use [`sturdyc.GetFetch`](https://pkg.go.dev/github.com/creativecreature/sturdyc#GetFetch) to have the cache fetch and store a record if it doesn't exist.
- Use [`sturdyc.GetFetchBatch`](https://pkg.go.dev/github.com/creativecreature/sturdyc#GetFetchBatch) to have the cache fetch and store each record in a batch if any of them doesn't exist.

To utilize these functions, you will first have to set up a client to manage
your configuration:

```go
func main() {
	// Maximum number of entries in the cache.
	capacity := 10000
	// Number of shards to use.
	numShards := 10
	// Time-to-live for cache entries.
	ttl := 2 * time.Hour
	// Percentage of entries to evict when the cache is full. Setting this
	// to 0 will make set a no-op when the cache has reached its capacity.
	// Expired records are evicted continiously by a background job.
	evictionPercentage := 10

	// Create a cache client with the specified configuration.
	cacheClient := sturdyc.New(capacity, numShards, ttl, evictionPercentage)

	// We can then use the client to store and retrieve values.
	sturdyc.Set(cacheClient, "key1", "value")
	if val, ok := sturdyc.Get[string](cacheClient, "key1"); ok {
		log.Println(val) // Prints "value"
	}

	sturdyc.Set(cacheClient, "key2", 1)
	if val, ok := sturdyc.Get[int](cacheClient, "key2"); ok {
		log.Println(val) // Prints 1
	}
}
```
