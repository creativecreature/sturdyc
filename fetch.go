package sturdyc

import (
	"context"
	"errors"
	"maps"
	"sync"
)

type call[T any] struct {
	sync.WaitGroup
	val T
	err error
}

func (c *Client[T]) finishCall(call *call[T], key string) {
	call.Done()
	c.Lock()
	delete(c.m, key)
	c.Unlock()
}

func fetchAndCache[V, T any](ctx context.Context, c *Client[T], key string, fn FetchFn[V]) (V, error) {
	c.Lock()
	if call, ok := c.m[key]; ok {
		c.Unlock()
		call.Wait()
		return unwrap[V, T](call.val, call.err)
	}

	call := new(call[T])
	call.Add(1)
	c.m[key] = call
	c.Unlock()

	response, err := fn(ctx)
	if err != nil && c.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
		c.SetMissing(key, *new(T), true)
		call.err = ErrMissingRecord
		c.finishCall(call, key)
		return response, ErrMissingRecord
	}

	if err != nil {
		call.err = err
		c.finishCall(call, key)
		return response, err
	}

	res, ok := any(response).(T)
	if !ok {
		call.err = ErrInvalidType
		c.finishCall(call, key)
		return response, ErrInvalidType
	}

	c.SetMissing(key, res, false)
	call.val, call.err = res, nil
	c.finishCall(call, key)

	return response, nil
}

func fetchAndCacheBatchTwo[V, T any](ctx context.Context, c *Client[T], ids []string, keyFn KeyFn, fetchFn BatchFetchFn[V]) (map[string]V, error) {
	c.Lock()

	inFlightIds := make([]string, 0, len(ids))
	notInFlightIds := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := c.m[keyFn(id)]; ok {
			inFlightIds = append(inFlightIds, id)
			continue
		}
		notInFlightIds = append(notInFlightIds, id)
	}
}

func fetchAndCacheBatch[V, T any](ctx context.Context, c *Client[T], ids []string, keyFn KeyFn, fetchFn BatchFetchFn[V]) (map[string]V, error) {
	response, err := fetchFn(ctx, ids)
	if err != nil {
		return response, err
	}

	// Check if we should store any of these IDs as a missing record.
	if c.storeMisses && len(response) < len(ids) {
		for _, id := range ids {
			if _, ok := response[id]; !ok {
				var zero T
				c.SetMissing(keyFn(id), zero, true)
			}
		}
	}

	// Store the records in the cache.
	for id, record := range response {
		v, ok := any(record).(T)
		if !ok {
			continue
		}
		c.SetMissing(keyFn(id), v, false)
	}

	return response, nil
}

func (c *Client[T]) groupIDs(ids []string, keyFn KeyFn) (hits map[string]T, misses, refreshes []string) {
	hits = make(map[string]T)
	misses = make([]string, 0)
	refreshes = make([]string, 0)

	for _, id := range ids {
		key := keyFn(id)
		value, exists, shouldIgnore, shouldRefresh := c.get(key)

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

// GetFetch attempts to retrieve the specified key from the cache. If the value
// is absent, it invokes the "fetchFn" function to obtain it and then stores
// the result. Additionally, when stampede protection is enabled, GetFetch
// determines if the record needs refreshing and, if necessary, schedules this
// task for background execution.
func (c *Client[T]) GetFetch(ctx context.Context, key string, fetchFn FetchFn[T]) (T, error) {
	// Begin by checking if we have the item in our cache.
	value, ok, shouldIgnore, shouldRefresh := c.get(key)

	if shouldRefresh {
		safeGo(func() {
			c.refresh(key, fetchFn)
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

// GetFetch is a convenience function that performs type assertion on the result of client.GetFetch.
func GetFetch[V, T any](ctx context.Context, c *Client[T], key string, fetchFn FetchFn[V]) (V, error) {
	return unwrap[V](c.GetFetch(ctx, key, wrap[T](fetchFn)))
}

// GetFetchBatch attempts to retrieve the specified ids from the cache. If any
// of the values are absent, it invokes the fetchFn function to obtain them and
// then stores the result. Additionally, when stampede protection is enabled,
// GetFetch determines if any of the records needs refreshing and, if
// necessary, schedules this to be performed in the background.
func (c *Client[T]) GetFetchBatch(ctx context.Context, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) (map[string]T, error) {
	cachedRecords, cacheMisses, idsToRefresh := c.groupIDs(ids, keyFn)

	// If any records need to be refreshed, we'll do so in the background.
	if len(idsToRefresh) > 0 {
		safeGo(func() {
			if c.bufferRefreshes {
				bufferBatchRefresh(c, idsToRefresh, keyFn, fetchFn)
				return
			}
			c.refreshBatch(idsToRefresh, keyFn, fetchFn)
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

// GetFetchBatch is a convenience function that performs type assertion on the result of client.GetFetchBatch.
func GetFetchBatch[V, T any](ctx context.Context, c *Client[T], ids []string, keyFn KeyFn, fetchFn BatchFetchFn[V]) (map[string]V, error) {
	return unwrapBatch[V](c.GetFetchBatch(ctx, ids, keyFn, wrapBatch[T](fetchFn)))
}
