# `sturdyc`: a caching library for building sturdy systems
[![Go Reference](https://pkg.go.dev/badge/github.com/creativecreature/sturdyc.svg)](https://pkg.go.dev/github.com/creativecreature/sturdyc)
[![Go Report Card](https://goreportcard.com/badge/github.com/creativecreature/sturdyc)](https://goreportcard.com/report/github.com/creativecreature/sturdyc)
[![codecov](https://codecov.io/gh/creativecreature/sturdyc/graph/badge.svg?token=CYSKW3Z7E6)](https://codecov.io/gh/creativecreature/sturdyc)

`Sturdyc` offers all the features you would expect from a caching library,
along with additional functionality designed to help you build robust
applications, such as:
- [**stampede protection**](https://github.com/creativecreature/sturdyc?tab=readme-ov-file#stampede-protection)
- [**caching non-existent records**](https://github.com/creativecreature/sturdyc?tab=readme-ov-file#non-existent-records)
- [**caching batch endpoints per record**](https://github.com/creativecreature/sturdyc?tab=readme-ov-file#batch-endpoints)
- **permutated cache keys**
- **refresh buffering**

# Installing
```sh
go get github.com/creativecreature/sturdyc
```

# At a glance
The package exports the following functions:
- Use [`sturdyc.Set`](https://pkg.go.dev/github.com/creativecreature/sturdyc#Set) to write a record to the cache.
- Use [`sturdyc.Get`](https://pkg.go.dev/github.com/creativecreature/sturdyc#Get) to get a record from the cache.
- Use [`sturdyc.GetFetch`](https://pkg.go.dev/github.com/creativecreature/sturdyc#GetFetch) to have the cache fetch and store a record.
- Use [`sturdyc.GetFetchBatch`](https://pkg.go.dev/github.com/creativecreature/sturdyc#GetFetchBatch) to have the cache fetch and store a batch of records.

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

Next, we'll look at some of the more advanced features.

# Stampede protection
Cache stampedes occur when many requests for a particular piece of data (which
has just expired or been evicted from the cache) come in at once. This can
cause all requests to fetch the data concurrently, subsequently causing a
significant load on the underlying data source.

To prevent this, we can enable **stampede protection**:

```go
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
	// used to spread out the refreshes for our entries evenly over time.
	minRefreshDelay := time.Millisecond * 10
	maxRefreshDelay := time.Millisecond * 30
	// The base for exponential backoff when retrying a refresh.
	retryBaseDelay := time.Millisecond * 10
	// NOTE: Ignore this for now, it will be shown in the next example.
	storeMisses := false

	// Create a cache client with the specified configuration.
	cacheClient := sturdyc.New(capacity, numShards, ttl, evictionPercentage,
		sturdyc.WithStampedeProtection(minRefreshDelay, maxRefreshDelay, retryBaseDelay, storeMisses),
	)
}
```

With this configuration, the cache will maintain fresh data that is renewed
every 10-30 milliseconds while protecting the actual data source. To
demonstrate, we can create this simple API client:

```go
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
```

and use it in our `main` function:

```go
func main() {
	// ...

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
```

Running this program, we're going to see output that looks something like this:

```sh
go run .
2024/04/07 09:05:29 Fetching value for key: key
2024/04/07 09:05:29 Value: value
2024/04/07 09:05:29 Value: value
2024/04/07 09:05:29 Fetching value for key: key
2024/04/07 09:05:29 Value: value
2024/04/07 09:05:29 Value: value
2024/04/07 09:05:29 Value: value
2024/04/07 09:05:29 Fetching value for key: key
```

The entire example is available [here.](https://github.com/creativecreature/sturdyc/tree/main/examples/stampede)

# Non-existent records
Another factor to consider is non-existent keys. It could be an ID that has
been added manually to a CMS with a typo that leads to no data being returned
from the upstream source. This can significantly increase our systems latency,
as we're never able to get a cache hit and serve from memory.

However, it could also be caused by a slow ingestion process. Perhaps it takes
some time for a new entity to propagate through a distributed system.

The cache allows us to store these IDs as missing records, while refreshing
them like any other record. To illustrate, we can extend the previous example
to enable this functionality:

```go
func main() {
	// ...

	// Tell the cache to store missing records.
	storeMisses := true

	// Create a cache client with the specified configuration.
	cacheClient := sturdyc.New(capacity, numShards, ttl, evictionPercentage,
		sturdyc.WithStampedeProtection(minRefreshDelay, maxRefreshDelay, retryBaseDelay, storeMisses),
	)

	// Create a new API instance with the cache client.
	api := NewAPI(cacheClient)

	// ...
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
```

Next, we'll modify the API client to return the `ErrStoreMissingRecord` error
for the first *3* calls. This error instructs the cache to store it as a missing
record:

```go
type API struct {
	count       int
	cacheClient *sturdyc.Client
}

func NewAPI(c *sturdyc.Client) *API {
	return &API{
		count:       0,
		cacheClient: c,
	}
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
	return sturdyc.GetFetch(ctx, a.cacheClient, key, fetchFn)
}
```

and then call it:

```go
func main() {
	// ...

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
```

```sh
2024/04/07 09:42:49 Fetching value for key: key
2024/04/07 09:42:49 Record does not exist.
2024/04/07 09:42:49 Record does not exist.
2024/04/07 09:42:49 Record does not exist.
2024/04/07 09:42:49 Fetching value for key: key
2024/04/07 09:42:49 Record does not exist.
2024/04/07 09:42:49 Record does not exist.
2024/04/07 09:42:49 Record does not exist.
2024/04/07 09:42:49 Fetching value for key: key
2024/04/07 09:42:49 Value: value
2024/04/07 09:42:49 Value: value
2024/04/07 09:42:49 Fetching value for key: key
```

## Batch endpoints
For batchable endpoints, you have to break the ids a part and cache them
individually. If you try to cache the entire batch, you'll quickly run into
problems. To illustrate, let's say that we have 10 000 records, and a endpoint
for fetching them that allows for batches up to 20. To calculate the number of
cache keys we can do this:

$$ C(n, k) = \binom{n}{k} = \frac{n!}{k!(n-k)!} $$

For $n = 10,000$ and $k = 20$, this becomes:

$$ C(10,000, 20) = \binom{10,000}{20} = \frac{10,000!}{20!(10,000-20)!} $$

This results in an approximate value of:

$$ \approx 4.032 \times 10^{61} $$

