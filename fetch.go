package sturdyc

import (
	"context"
	"errors"
)

func fetchAndCache[T, V any](ctx context.Context, c *Cache[T], key string, fetchFn FetchFn[V]) (V, error) {
	response, err := fetchFn(ctx)
	res, ok := any(response).(T)
	if !ok {
		return response, errors.New("invalid response type")
	}

	if err != nil && c.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
		c.set(key, res, true)
		return response, ErrMissingRecord
	}

	if err != nil {
		return response, err
	}

	c.set(key, res, false)
	return response, nil
}

func fetchAndCacheBatch[T, V any](ctx context.Context, c *Cache[T], ids []string, keyFn KeyFn, fetchFn BatchFetchFn[V]) (map[string]V, error) {
	response, err := fetchFn(ctx, ids)
	if err != nil {
		return response, err
	}

	// Check if we should store any of these IDs as a missing record.
	if c.storeMisses && len(response) < len(ids) {
		for _, id := range ids {
			if _, ok := response[id]; !ok {
				var zero T
				c.set(keyFn(id), zero, true)
			}
		}
	}

	// Store the records in the cache.
	for id, record := range response {
		v, ok := any(record).(T)
		if !ok {
			continue
		}
		c.set(keyFn(id), v, false)
	}

	return response, nil
}
