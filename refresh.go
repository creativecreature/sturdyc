package sturdyc

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (c *Client[T]) refresh(key string, fetchFn FetchFn[T]) {
	response, err := fetchFn(context.Background())
	if err != nil {
		if c.storeMissingRecords && errors.Is(err, ErrNotFound) {
			c.StoreMissingRecord(key)
		}
		if !c.storeMissingRecords && errors.Is(err, ErrNotFound) {
			c.Delete(key)
		}
		return
	}
	c.Set(key, response)
}

func (c *Client[T]) refreshBatch(ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) {
	c.reportBatchRefreshSize(len(ids))
	response, err := fetchFn(context.Background(), ids)
	if err != nil {
		return
	}

	// Check if any of the records have been deleted at the data source.
	responseIDs := make([]string, 0, len(response))
	for id := range response {
		responseIDs = append(responseIDs, id)
	}
	for _, id := range ids {
		_, okCache, _, _ := c.getWithState(keyFn(id))
		_, okResponse := response[id]

		if okResponse {
			continue
		}

		if !c.storeMissingRecords && !okResponse && okCache {
			c.Delete(keyFn(id))
		}

		if c.storeMissingRecords && !okResponse {
			c.log.Warn(fmt.Sprintf(
				"storing %s as missing after a refresh. Requested ids: %s. Received ids: %s",
				id, strings.Join(ids, ","), strings.Join(responseIDs, ","),
			))
			c.StoreMissingRecord(keyFn(id))
		}
	}

	// Cache the refreshed records.
	for id, record := range response {
		c.Set(keyFn(id), record)
	}
}
