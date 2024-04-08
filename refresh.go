package sturdyc

import (
	"context"
	"errors"
)

func refresh[T any](client *Client, key string, fetchFn FetchFn[T]) {
	response, err := fetchFn(context.Background())
	if err != nil {
		// Check if it is a missing record, and if we should store it with a cooldown.
		if client.storeMisses && errors.Is(err, ErrStoreMissingRecord) {
			client.set(key, response, true)
		}
		return
	}
	client.set(key, response, false)
}

func refreshBatch[T any](client *Client, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) {
	if client.metricsRecorder != nil {
		client.metricsRecorder.CacheBatchRefreshSize(len(ids))
	}

	response, err := fetchFn(context.Background(), ids)
	if err != nil {
		return
	}

	if client.storeMisses && len(response) < len(ids) {
		for _, id := range ids {
			if v, ok := response[id]; !ok {
				client.set(keyFn(id), v, true)
			}
		}
	}

	// Cache the refreshed records.
	for id, record := range response {
		client.set(keyFn(id), record, false)
	}
}
