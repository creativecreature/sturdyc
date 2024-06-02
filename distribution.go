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
	Set(key string, value []byte)
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

func (c *Client[T]) marshalRecord(value T) ([]byte, error) {
	record := distributedRecord[T]{CreatedAt: c.clock.Now(), Value: value, IsMissingRecord: false}
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

func (c *Client[T]) distributedFetch(key string, fetchFn FetchFn[T]) FetchFn[T] {
	if c.distributedStorage == nil {
		return fetchFn
	}

	return func(ctx context.Context) (T, error) {
		bytes, ok := c.distributedStorage.Get(ctx, key)

		if !ok {
			record, err := fetchFn(ctx)

			// If the record does not exist in the distributed storage, and the fetch function
			// returned an error that isn't ErrStoreMissingRecord we'll exit early.
			if err != nil && !errors.Is(err, ErrStoreMissingRecord) {
				return record, err
			}

			// Check if we should store the record as missing in the distributed storage too.
			if err != nil && errors.Is(err, ErrStoreMissingRecord) {
				if !c.storeMissingRecords {
					c.log.Error("sturdyc: store missing record requested but missing record storage is not enabled")
					return record, err
				}
				c.safeGo(func() {
					if missingRecordBytes, missingRecordErr := c.marshalMissingRecord(); missingRecordErr == nil {
						c.distributedStorage.Set(key, missingRecordBytes)
					}
				})
				return record, ErrMissingRecord
			}

			// At this point, we should have handled all the errors.
			c.safeGo(func() {
				if recordBytes, marshalErr := c.marshalRecord(record); marshalErr == nil {
					c.distributedStorage.Set(key, recordBytes)
				}
			})
			return record, nil
		}

		var record distributedRecord[T]
		unmarshalErr := json.Unmarshal(bytes, &record)
		if unmarshalErr != nil {
			c.log.Error(fmt.Sprintf("sturdyc: error unmarshalling record: %v", unmarshalErr))
		}

		// Check if we could parse the record and it was fresh.
		if unmarshalErr == nil && (!c.distributedStaleStorage || (time.Since(record.CreatedAt) < c.distributedStaleDuration)) {
			if record.IsMissingRecord {
				return record.Value, ErrMissingRecord
			}
			return record.Value, nil
		}

		fetchedRecord, fetchErr := fetchFn(ctx)
		if errors.Is(fetchErr, ErrStoreMissingRecord) {
			c.safeGo(func() {
				if !c.storeMissingRecords {
					c.log.Error("sturdyc: store missing record requested but missing record storage is not enabled")
					return
				}
				if missingRecordBytes, missingRecordErr := c.marshalMissingRecord(); missingRecordErr == nil {
					c.distributedStorage.Set(key, missingRecordBytes)
				}
			})
			return fetchedRecord, unmarshalErr
		}

		if errors.Is(fetchErr, ErrDeleteRecord) {
			c.safeGo(func() {
				c.distributedStorage.Delete(context.Background(), key)
			})
			return fetchedRecord, unmarshalErr
		}

		if fetchErr != nil {
			if unmarshalErr == nil {
				return record.Value, nil
			}
			return fetchedRecord, fetchErr
		}

		c.safeGo(func() {
			if recordBytes, marshalErr := c.marshalRecord(fetchedRecord); marshalErr == nil {
				c.distributedStorage.Set(key, recordBytes)
			}
		})
		return fetchedRecord, nil
	}
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

		// Group the records we got from the distributed storage into fresh/stale maps.
		distributedRecords := c.distributedStorage.GetBatch(ctx, keys)
		fresh := make(map[string]T, len(ids))
		stale := make(map[string]T, len(ids))
		for key, bytes := range distributedRecords {
			var record distributedRecord[T]
			unmarshalErr := json.Unmarshal(bytes, &record)
			if unmarshalErr != nil {
				c.log.Error(fmt.Sprintf("sturdyc: error unmarshalling distributed record: %v", unmarshalErr))
				continue
			}

			if record.IsMissingRecord {
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

		// Next, we'll want to check if we should change any of the records to be missing or perform deletions.
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
				if c.storeMissingRecords {
					missingRecords := make(map[string][]byte, len(idsToDelete))
					for _, id := range idsToDelete {
						if bytes, err := c.marshalMissingRecord(); err == nil {
							missingRecords[keyFn(id)] = bytes
						}
					}
					c.distributedStorage.SetBatch(context.Background(), missingRecords)
					return
				}
				c.distributedStorage.DeleteBatch(context.Background(), idsToDelete)
			})
		}

		// Write the records we retrieved to the distributed storage.
		recordsToWrite := make(map[string][]byte, len(dataSourceRecords))
		for id, record := range dataSourceRecords {
			if recordBytes, marshalErr := c.marshalRecord(record); marshalErr == nil {
				recordsToWrite[keyFn(id)] = recordBytes
			}
		}

		if c.storeMissingRecords {
			for _, id := range idsToRefresh {
				if _, ok := dataSourceRecords[id]; !ok {
					if bytes, err := c.marshalMissingRecord(); err == nil {
						recordsToWrite[keyFn(id)] = bytes
					}
				}
			}
		}

		c.safeGo(func() {
			c.distributedStorage.SetBatch(context.Background(), recordsToWrite)
		})

		maps.Copy(fresh, dataSourceRecords)
		return fresh, nil
	}
}
