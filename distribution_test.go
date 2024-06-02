package sturdyc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/creativecreature/sturdyc"
)

type mockStorage struct {
	sync.Mutex
	records map[string][]byte
}

func (m *mockStorage) Get(_ context.Context, _ string) ([]byte, bool) {
	panic("not implemented")
}

func (m *mockStorage) Set(_ string, _ []byte) {
	panic("not implemented")
}

func (m *mockStorage) Delete(_ context.Context, _ string) {
	panic("not implemented")
}

func (m *mockStorage) GetBatch(_ context.Context, _ []string) map[string][]byte {
	m.Lock()
	defer m.Unlock()
	return m.records
}

func (m *mockStorage) SetBatch(_ context.Context, records map[string][]byte) {
	m.Lock()
	defer m.Unlock()

	if m.records == nil {
		m.records = records
		return
	}

	for key, value := range records {
		m.records[key] = value
	}
}

func (m *mockStorage) assertRecords(t *testing.T, ids []string, keyFn sturdyc.KeyFn) {
	t.Helper()

	m.Lock()
	defer m.Unlock()

	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, keyFn(id))
	}

	for _, key := range keys {
		if _, ok := m.records[key]; !ok {
			t.Errorf("expected key %s to be in records", key)
		}
	}
}

// NOTE: I could delete this for now, but I'm going to need it later.
func (m *mockStorage) DeleteBatch(_ context.Context, keys []string) error {
	m.Lock()
	defer m.Unlock()
	for _, key := range keys {
		delete(m.records, key)
	}
	return nil
}

func TestDistributedStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ttl := time.Minute
	distributedStorage := &mockStorage{}
	c := sturdyc.New[string](1000, 10, ttl, 30,
		sturdyc.WithDistributedStorage(distributedStorage),
	)
	fetchObserver := NewFetchObserver(1)

	keyFn := c.BatchKeyFn("item")
	firstBatchOfIDs := []string{"1", "2", "3"}
	fetchObserver.BatchResponse(firstBatchOfIDs)
	_, err := sturdyc.GetFetchBatch(ctx, c, firstBatchOfIDs, keyFn, fetchObserver.FetchBatch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// TODO: We don't have to wait for the channel here, but let's keep it and
	// remove it after we've written tests for the background refresh logic.
	<-fetchObserver.FetchCompleted
	fetchObserver.AssertRequestedRecords(t, firstBatchOfIDs)
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecords(t, firstBatchOfIDs, keyFn)

	// Next, we'll delete the records from the in-memory cache to simulate that they were evicted.
	for _, id := range firstBatchOfIDs {
		c.Delete(keyFn(id))
	}
	if c.Size() != 0 {
		t.Fatalf("expected cache size to be 0, got %d", c.Size())
	}

	// Now we can request a second batch of IDs. The fetchObservers
	// FetchBatch function should not get called for IDs 1-3.
	fetchObserver.BatchResponse([]string{"4", "5", "6"})
	secondBatchOfIDs := []string{"1", "2", "3", "4", "5", "6"}
	res, err := sturdyc.GetFetchBatch(ctx, c, secondBatchOfIDs, keyFn, fetchObserver.FetchBatch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, id := range secondBatchOfIDs {
		if _, ok := res[id]; !ok {
			t.Errorf("expected id %s to be in the response", id)
		}
	}

	// TODO: We don't have to wait for the channel here, but let's keep it and
	// remove it after we've written tests for the background refresh logic.
	<-fetchObserver.FetchCompleted
	fetchObserver.AssertRequestedRecords(t, []string{"4", "5", "6"})
	fetchObserver.AssertFetchCount(t, 2)

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecords(t, secondBatchOfIDs, keyFn)
}
