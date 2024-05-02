package sturdyc

import (
	"context"
	"errors"
)

func fetchAndCache[T any](ctx context.Context, c *Client, key string, fetchFn FetchFn[T]) (T, error) {
	response, err := fetchFn(ctx)
	if err != nil && c.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
		c.set(key, response, true)
		return response, ErrMissingRecord
	}

	if err != nil {
		return response, err
	}

	c.set(key, response, false)
	return response, nil
}

func fetchAndCacheBatch[T any](ctx context.Context, c *Client, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) (map[string]T, error) {
	response, err := fetchFn(ctx, ids)
	if err != nil {
		return response, err
	}

	// Check if we should store any of these IDs as a missing record.
	if c.storeMisses && len(response) < len(ids) {
		for _, id := range ids {
			if v, ok := response[id]; !ok {
				c.set(keyFn(id), v, true)
			}
		}
	}

	// Store the records in the cache.
	for id, record := range response {
		c.set(keyFn(id), record, false)
	}

	return response, nil
}
