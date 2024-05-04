package sturdyc

import (
	"context"
	"maps"
)

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

func groupIDs[T any](c *Client, ids []string, keyFn KeyFn) (hits map[string]T, misses, refreshes []string) {
	hits = make(map[string]T)
	misses = make([]string, 0)
	refreshes = make([]string, 0)

	for _, id := range ids {
		key := keyFn(id)
		value, exists, shouldIgnore, shouldRefresh := get[T](c, key)

		// Check if the record should be refreshed in the background.
		if shouldRefresh {
			refreshes = append(refreshes, id)
		}

		if shouldIgnore {
			continue
		}

		if !exists {
			misses = append(misses, id)
			continue
		}

		hits[id] = value
	}
	return hits, misses, refreshes
}

// Get retrieves a value from the cache and performs a type assertion to the desired type.
func Get[T any](c *Client, key string) (T, bool) {
	value, ok, _, _ := get[T](c, key)
	return value, ok
}

// GetFetch attempts to retrieve the specified key from the cache. If the value
// is absent, it invokes the "fetchFn" function to obtain it and then stores
// the result. Additionally, when stampede protection is enabled, GetFetch
// determines if the record needs refreshing and, if necessary, schedules this
// task for background execution.
func GetFetch[T any](ctx context.Context, c *Client, key string, fetchFn FetchFn[T]) (T, error) {
	// Begin by checking if we have the item in our cache.
	value, ok, shouldIgnore, shouldRefresh := get[T](c, key)

	if shouldRefresh {
		safeGo(func() {
			refresh(c, key, fetchFn)
		})
	}

	if shouldIgnore {
		return value, ErrMissingRecord
	}

	if ok {
		return value, nil
	}

	return fetchAndCache(ctx, c, key, fetchFn)
}

// GetFetchBatch attempts to retrieve the specified ids from the cache. If any
// of the values are absent, it invokes the fetchFn function to obtain them and
// then stores the result. Additionally, when stampede protection is enabled,
// GetFetch determines if any of the records needs refreshing and, if
// necessary, schedules this to be performed in the background.
func GetFetchBatch[T any](ctx context.Context, c *Client, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) (map[string]T, error) {
	cachedRecords, cacheMisses, idsToRefresh := groupIDs[T](c, ids, keyFn)

	// If any records need to be refreshed, we'll do so in the background.
	if len(idsToRefresh) > 0 {
		safeGo(func() {
			if c.bufferRefreshes {
				bufferBatchRefresh(c, idsToRefresh, keyFn, fetchFn)
				return
			}
			refreshBatch(c, idsToRefresh, keyFn, fetchFn)
		})
	}

	// If we were able to retrieve all records from the cache, we can return them straight away.
	if len(cacheMisses) == 0 {
		return cachedRecords, nil
	}

	response, err := fetchAndCacheBatch(ctx, c, cacheMisses, keyFn, fetchFn)
	if err != nil {
		if len(cachedRecords) > 0 {
			return cachedRecords, ErrOnlyCachedRecords
		}
		return cachedRecords, err
	}

	maps.Copy(cachedRecords, response)
	return cachedRecords, nil
}
