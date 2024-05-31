package sturdyc

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"
)

type distributedRecord[T any] struct {
	CreatedAt time.Time `json:"created_at"`
	Value     T         `json:"value"`
}

type DistributedStorage interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(ctx context.Context, key string) error

	GetBatch(ctx context.Context, keys []string) (map[string][]byte, error)
	SetBatch(ctx context.Context, records map[string][]byte) error
	DeleteBatch(ctx context.Context, keys []string) error
}

func (c *Client[T]) distributedBatchFetch(keyFn KeyFn, fetchFn BatchFetchFn[T]) BatchFetchFn[T] {
	if c.distributedStorage == nil {
		return fetchFn
	}

	return func(ctx context.Context, ids []string) (map[string]T, error) {
		// We need to be able to lookup the ID of the record based on the key.
		keyIDMap := make(map[string]string, len(ids))
		keys := make([]string, 0, len(ids))
		for _, id := range ids {
			key := keyFn(id)
			keyIDMap[key] = id
			keys = append(keys, key)
		}

		distributedRecords, err := c.distributedStorage.GetBatch(ctx, keys)
		if err != nil {
			c.log.Error(fmt.Sprintf("sturdyc: error refreshing batch from distributed storage: %v", err))
		}

		// Group the records we got from the distributed storage into fresh/stale maps.
		fresh := make(map[string]T, len(ids))
		stale := make(map[string]T, len(ids))
		for key, bytes := range distributedRecords {
			var record distributedRecord[T]
			unmarshalErr := json.Unmarshal(bytes, &record)
			if unmarshalErr != nil {
				c.log.Error(fmt.Sprintf("sturdyc: error unmarshalling distributed record: %v", unmarshalErr))
				continue
			}

			if !c.distributedStaleStorage {
				c.Set(key, record.Value)
				fresh[keyIDMap[key]] = record.Value
				continue
			}

			if time.Since(record.CreatedAt) < c.distributedStaleDuration {
				c.Set(key, record.Value)
				fresh[keyIDMap[key]] = record.Value
				continue
			}

			stale[keyIDMap[key]] = record.Value
		}

		// The IDs that we need to get from the underlying data source are the ones that are stale or missing.
		idsToRefresh := make([]string, 0, len(ids)-len(fresh))
		for _, id := range ids {
			if _, ok := fresh[id]; ok {
				continue
			}
			idsToRefresh = append(idsToRefresh, id)
		}

		dataSourceRecords, err := fetchFn(ctx, idsToRefresh)
		// Incase of an error, we'll proceed with the ones we got from the distributed storage.
		if err != nil {
			c.log.Error(fmt.Sprintf("sturdyc: error fetching records from the underlying data source. %v", err))
			maps.Copy(stale, fresh)
			return stale, nil
		}

		// Next, we'll want to check if we should perform any deletions.
		idsToDelete := make([]string, 0)
		for _, id := range idsToRefresh {
			_, ok := dataSourceRecords[id]
			_, okStale := stale[keyFn(id)]
			if !ok && okStale {
				idsToDelete = append(idsToDelete, keyFn(id))
			}
		}
		if len(idsToDelete) > 0 {
			// We don't need the user to wait for these and we don't want panics to bring down the server.
			c.safeGo(func() {
				err := c.distributedStorage.DeleteBatch(context.Background(), idsToDelete)
				if err != nil {
					c.log.Error(fmt.Sprintf("sturdyc: error deleting records from the distributed storage: %v", err))
				}
			})
		}

		maps.Copy(fresh, dataSourceRecords)
		return fresh, nil
	}
}
