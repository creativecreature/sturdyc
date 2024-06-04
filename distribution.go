package sturdyc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"
)

type distributedRecord[T any] struct {
	CreatedAt       time.Time `json:"created_at"`
	Value           T         `json:"value"`
	IsMissingRecord bool      `json:"is_missing_record"`
}

type DistributedStorage interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte)
	GetBatch(ctx context.Context, keys []string) map[string][]byte
	SetBatch(ctx context.Context, records map[string][]byte)
}

type DistributedStaleStorage interface {
	DistributedStorage
	Delete(ctx context.Context, key string)
	DeleteBatch(ctx context.Context, keys []string)
}

type distributedStorage struct {
	DistributedStorage
}

func (d *distributedStorage) Delete(_ context.Context, _ string) {
}

func (d *distributedStorage) DeleteBatch(_ context.Context, _ []string) {
}

func marshalRecord[V, T any](value V, c *Client[T]) ([]byte, error) {
	record := distributedRecord[V]{CreatedAt: c.clock.Now(), Value: value, IsMissingRecord: false}
	bytes, err := json.Marshal(record)
	if err != nil {
		c.log.Error(fmt.Sprintf("sturdyc: error marshalling record: %v", err))
	}
	return bytes, err
}

func (c *Client[T]) marshalMissingRecord() ([]byte, error) {
	var missingRecord distributedRecord[T]
	missingRecord.CreatedAt = c.clock.Now()
	missingRecord.IsMissingRecord = true
	bytes, err := json.Marshal(missingRecord)
	if err != nil {
		c.log.Error(fmt.Sprintf("sturdyc: error marshalling missing record: %v", err))
	}
	return bytes, err
}

func unmarshalRecord[V any](bytes []byte, key string, log Logger) (distributedRecord[V], error) {
	var record distributedRecord[V]
	unmarshalErr := json.Unmarshal(bytes, &record)
	if unmarshalErr != nil {
		log.Error("sturdyc: error unmarshalling key: " + key)
	}
	return record, unmarshalErr
}

func (c *Client[T]) writeMissingRecord(key string) {
	c.safeGo(func() {
		if missingRecordBytes, missingRecordErr := c.marshalMissingRecord(); missingRecordErr == nil {
			c.distributedStorage.Set(context.Background(), key, missingRecordBytes)
		}
	})
}

func distributedFetch[V, T any](c *Client[T], key string, fetchFn FetchFn[V]) FetchFn[V] {
	if c.distributedStorage == nil {
		return fetchFn
	}

	return func(ctx context.Context) (V, error) {
		var stale V
		hasStale := false
		if bytes, ok := c.distributedStorage.Get(ctx, key); ok {
			record, unmarshalErr := unmarshalRecord[V](bytes, key, c.log)
			if unmarshalErr != nil {
				return record.Value, unmarshalErr
			}

			// Check if the record is fresh enough to not need a refresh.
			if !c.distributedStaleStorage || c.clock.Since(record.CreatedAt) < c.distributedStaleDuration {
				if record.IsMissingRecord {
					return record.Value, ErrNotFound
				}
				return record.Value, nil
			}

			stale = record.Value
			hasStale = true
		}

		// If it's not fresh enough, we'll retrieve it from the source.
		response, fetchErr := fetchFn(ctx)
		if fetchErr == nil {
			c.safeGo(func() {
				if recordBytes, marshalErr := marshalRecord(response, c); marshalErr == nil {
					c.distributedStorage.Set(context.Background(), key, recordBytes)
				}
			})
			return response, nil
		}

		if errors.Is(fetchErr, ErrNotFound) {
			if c.storeMissingRecords {
				c.writeMissingRecord(key)
				return response, fetchErr
			}
			if hasStale {
				c.safeGo(func() {
					c.distributedStorage.Delete(context.Background(), key)
				})
			}
			return response, fetchErr
		}

		if hasStale {
			return stale, nil
		}

		return response, fetchErr
	}
}

func distributedBatchFetch[V, T any](c *Client[T], keyFn KeyFn, fetchFn BatchFetchFn[V]) BatchFetchFn[V] {
	if c.distributedStorage == nil {
		return fetchFn
	}

	return func(ctx context.Context, ids []string) (map[string]V, error) {
		// We need to be able to lookup the ID of the record based on the key.
		keyIDMap := make(map[string]string, len(ids))
		keys := make([]string, 0, len(ids))
		for _, id := range ids {
			key := keyFn(id)
			keyIDMap[key] = id
			keys = append(keys, key)
		}

		distributedRecords := c.distributedStorage.GetBatch(ctx, keys)
		// Group the records we got from the distributed storage into fresh/stale maps.
		fresh := make(map[string]V, len(ids))
		stale := make(map[string]V, len(ids))

		// The IDs that we need to get from the underlying data source are the ones that are stale or missing.
		idsToRefresh := make([]string, 0, len(ids))
		for _, id := range ids {
			key := keyFn(id)
			bytes, ok := distributedRecords[key]
			if !ok {
				idsToRefresh = append(idsToRefresh, id)
				continue
			}

			record, unmarshalErr := unmarshalRecord[V](bytes, key, c.log)
			if unmarshalErr != nil {
				idsToRefresh = append(idsToRefresh, id)
				continue
			}

			// If distributedStaleStorage isn't enabled it means all records are fresh, otherwise checked the CreatedAt time.
			if !c.distributedStaleStorage || c.clock.Since(record.CreatedAt) < c.distributedStaleDuration {
				// We never wan't to return missing records.
				if !record.IsMissingRecord {
					fresh[id] = record.Value
				}
				continue
			}

			idsToRefresh = append(idsToRefresh, id)
			// We never wan't to return missing records.
			if !record.IsMissingRecord {
				stale[id] = record.Value
			}
		}

		if len(idsToRefresh) == 0 {
			return fresh, nil
		}

		dataSourceResponses, err := fetchFn(ctx, idsToRefresh)
		// Incase of an error, we'll proceed with the ones we got from the distributed storage.
		if err != nil {
			c.log.Error(fmt.Sprintf("sturdyc: error fetching records from the underlying data source. %v", err))
			maps.Copy(stale, fresh)
			return stale, nil
		}

		// Next, we'll want to check if we should change any of the records to be missing or perform deletions.
		recordsToWrite := make(map[string][]byte, len(dataSourceResponses))
		keysToDelete := make([]string, 0, len(idsToRefresh)-len(dataSourceResponses))
		for _, id := range idsToRefresh {
			key := keyFn(id)
			response, ok := dataSourceResponses[id]

			if ok {
				if recordBytes, marshalErr := marshalRecord(response, c); marshalErr == nil {
					recordsToWrite[key] = recordBytes
				}
				continue
			}

			// At this point, we know that we weren't able to retrieve this ID from the underlying data source.
			if c.storeMissingRecords {
				if bytes, err := c.marshalMissingRecord(); err == nil {
					recordsToWrite[key] = bytes
				}
				continue
			}

			// If the record exists in the distributed storage but not at the underlying data source, we'll have to delete it.
			if _, okStale := stale[id]; okStale {
				keysToDelete = append(keysToDelete, key)
			}
		}

		if len(keysToDelete) > 0 {
			c.safeGo(func() {
				c.distributedStorage.DeleteBatch(context.Background(), keysToDelete)
			})
		}

		if len(recordsToWrite) > 0 {
			c.safeGo(func() {
				c.distributedStorage.SetBatch(context.Background(), recordsToWrite)
			})
		}

		maps.Copy(fresh, dataSourceResponses)
		return fresh, nil
	}
}