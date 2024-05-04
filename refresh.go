package sturdyc

import (
	"context"
	"errors"
)

func refresh[T any](c *Client, key string, fetchFn FetchFn[T]) {
	response, err := fetchFn(context.Background())
	if err != nil {
		if c.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
			c.set(key, response, true)
		}
		return
	}
	c.set(key, response, false)
}

func refreshBatch[T any](c *Client, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) {
	if c.metricsRecorder != nil {
		c.metricsRecorder.CacheBatchRefreshSize(len(ids))
	}

	response, err := fetchFn(context.Background(), ids)
	if err != nil {
		return
	}

	if c.storeMisses && len(response) < len(ids) {
		for _, id := range ids {
			if v, ok := response[id]; !ok {
				c.set(keyFn(id), v, true)
			}
		}
	}

	// Cache the refreshed records.
	for id, record := range response {
		c.set(keyFn(id), record, false)
	}
}
