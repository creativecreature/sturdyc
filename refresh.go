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

	// Check if any of the records have been deleted at the data source.
	for _, id := range ids {
		_, okCache := c.get(keyFn(id))
		v, okResponse := response[id]

		if okResponse {
			continue
		}

		if !c.storeMisses && !okResponse && okCache {
			c.Delete(keyFn(id))
		}

		if c.storeMisses && !okResponse {
			c.set(keyFn(id), v, true)
		}
	}

	// Cache the refreshed records.
	for id, record := range response {
		c.set(keyFn(id), record, false)
	}
}
