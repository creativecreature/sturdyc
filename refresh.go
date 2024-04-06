package sturdyc

import (
	"context"
	"errors"
	"time"
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

// deleteRefreshBuffer should be called WITH a lock when a buffer has been processed.
func deleteRefreshBuffer(c *Client, batchIdentifier string) {
	delete(c.bufferIdentifierChans, batchIdentifier)
	delete(c.bufferIdentifierIDs, batchIdentifier)
}

func bufferBatchRefresh[T any](c *Client, ids []string, keyFn KeyFn, fetchFn BatchFetchFn[T]) {
	if len(ids) == 0 {
		return
	}

	// If we got a perfect batch size, we can refresh the records immediately.
	if len(ids) == c.maxBufferSize {
		refreshBatch(c, ids, keyFn, fetchFn)
		return
	}

	c.bufferMutex.Lock()

	// If the ids are greater than our ideal buffer size we'll refresh
	// some of them, and create a new buffer for the remainder.
	if len(ids) > c.maxBufferSize {
		idsToRefresh, overflowingIDs := ids[:c.maxBufferSize], ids[c.maxBufferSize:]
		c.bufferMutex.Unlock()
		safeGo(func() {
			refreshBatch(c, idsToRefresh, keyFn, fetchFn)
		})
		safeGo(func() {
			bufferBatchRefresh(c, overflowingIDs, keyFn, fetchFn)
		})
		return
	}

	// Extract the cache key prefix that uniquely identifies this group of records.
	keyPrefix := extractPermutation(keyFn(ids[0]))

	// Check if we already have a batch waiting to be refreshed.
	if channel, ok := c.bufferIdentifierChans[keyPrefix]; ok {
		// There is a small chance that another goroutine manages to write to the channel
		// and fill the buffer as we unlock this mutex. Therefore, we'll add a timer so
		// that we can process these ids again if that were to happen.
		c.bufferMutex.Unlock()
		timer, stop := c.clock.NewTimer(time.Millisecond * 10)
		select {
		case channel <- ids:
			stop()
		case <-timer:
			safeGo(func() {
				bufferBatchRefresh(c, ids, keyFn, fetchFn)
			})
			return
		}
		return
	}

	// There is no existing batch buffering for this key, so we'll create a new one.
	newChannel := make(chan []string)
	c.bufferIdentifierChans[keyPrefix] = newChannel
	c.bufferIdentifierIDs[keyPrefix] = ids

	safeGo(func() {
		c.bufferMutex.Unlock()
		timer, stop := c.clock.NewTimer(c.bufferTimeout)

		for {
			select {
			// If the buffer times out, we'll refresh the records regardless of the buffer size.
			case _, ok := <-timer:
				if !ok {
					return
				}

				c.bufferMutex.Lock()
				idsToRefresh := c.bufferIdentifierIDs[keyPrefix]
				deleteRefreshBuffer(c, keyPrefix)
				c.bufferMutex.Unlock()
				safeGo(func() {
					refreshBatch(c, idsToRefresh, keyFn, fetchFn)
				})
				return

			case newIDs, ok := <-newChannel:
				if !ok {
					return
				}

				c.bufferMutex.Lock()
				c.bufferIdentifierIDs[keyPrefix] = append(c.bufferIdentifierIDs[keyPrefix], newIDs...)

				// If we haven't reached the buffer size yet, we'll wait for more ids.
				if len(c.bufferIdentifierIDs[keyPrefix]) < c.maxBufferSize {
					c.bufferMutex.Unlock()
					continue
				}

				// We have reached or exceeded the buffer size. We'll stop the timer and drain the channel.
				if !stop() {
					<-timer
				}

				allIDs := c.bufferIdentifierIDs[keyPrefix]
				deleteRefreshBuffer(c, keyPrefix)
				c.bufferMutex.Unlock()
				idsToRefresh, overflowingIDs := allIDs[:c.maxBufferSize], allIDs[c.maxBufferSize:]
				safeGo(func() {
					refreshBatch(c, idsToRefresh, keyFn, fetchFn)
				})
				safeGo(func() {
					bufferBatchRefresh(c, overflowingIDs, keyFn, fetchFn)
				})
				return
			}
		}
	})
}
