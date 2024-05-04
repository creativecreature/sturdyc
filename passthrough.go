package sturdyc

import (
	"context"
	"math/rand/v2"
)

// Passthrough attempts to retrieve the id from the cache. If looking up the ID
// results in a cache miss, it will fetch the record using the fetchFn. If the
// record was found in the cache, it will perform another check to determine if
// it should allow a request to passthrough to the underlying data source. This
// is performed in the background, and the cache cached record will be updated
// with the response.
func Passthrough[T any](ctx context.Context, c *Client, key string, fetchFn FetchFn[T]) (T, error) {
	if value, ok, _, _ := get[T](c, key); ok {
		// Check if we should do a passthrough.
		if c.passthroughPercentage >= 100 || rand.IntN(100) >= c.passthroughPercentage {
			safeGo(func() {
				refresh(c, key, fetchFn)
			})
		}
		return value, nil
	}
	return fetchAndCache(ctx, c, key, fetchFn)
}

// PassthroughBatch attempts to retrieve the ids from the cache. If looking up
// any of the IDs results in a cache miss, it will fetch the batch using the
// fetchFn. If all of the ID's are found in the cache, it will perform another
// check to determine if it should allow a request to passthrough to the
// underlying data source. This is performed in the background, and the cache
// will be updated with the response.
func PassthroughBatch[T any](ctx context.Context, c *Client, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) (map[string]T, error) {
	cachedRecords, cacheMisses, _ := groupIDs[T](c, ids, keyFn)

	// If we have cache misses, we're going to perform an outgoing refresh
	// regardless. We'll utilize this to perform a passthrough for all IDs.
	if len(cacheMisses) > 0 {
		res, err := fetchAndCacheBatch(ctx, c, ids, keyFn, fetchFn)
		if err != nil && len(cachedRecords) > 0 {
			return cachedRecords, ErrOnlyCachedRecords
		}
		return res, err
	}

	// Check if we should do a passthrough.
	if c.passthroughPercentage >= 100 || rand.IntN(100) >= c.passthroughPercentage {
		safeGo(func() {
			if c.passthroughBuffering {
				bufferBatchRefresh(c, ids, keyFn, fetchFn)
				return
			}
			refreshBatch(c, ids, keyFn, fetchFn)
		})
	}

	return cachedRecords, nil
}
