package sturdyc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/creativecreature/sturdyc"
)

type mockStorage struct {
	getCount    int
	setCount    int
	deleteCount int
	sync.Mutex
	records map[string][]byte
}

func (m *mockStorage) Get(_ context.Context, key string) ([]byte, bool) {
	m.Lock()
	defer m.Unlock()
	m.getCount++

	bytes, ok := m.records[key]
	return bytes, ok
}

func (m *mockStorage) Set(_ context.Context, key string, bytes []byte) {
	m.Lock()
	defer m.Unlock()
	m.setCount++

	if m.records == nil {
		m.records = make(map[string][]byte)
	}
	m.records[key] = bytes
}

func (m *mockStorage) Delete(_ context.Context, key string) {
	m.Lock()
	defer m.Unlock()
	m.deleteCount++
	delete(m.records, key)
}

func (m *mockStorage) GetBatch(_ context.Context, _ []string) map[string][]byte {
	m.Lock()
	defer m.Unlock()
	m.getCount++
	return m.records
}

func (m *mockStorage) SetBatch(_ context.Context, records map[string][]byte) {
	m.Lock()
	defer m.Unlock()
	m.setCount++

	if m.records == nil {
		m.records = records
		return
	}

	for key, value := range records {
		m.records[key] = value
	}
}

func (m *mockStorage) assertRecord(t *testing.T, key string) {
	t.Helper()

	m.Lock()
	defer m.Unlock()

	if _, ok := m.records[key]; !ok {
		t.Errorf("expected key %s to be in records", key)
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

func (m *mockStorage) assertGetCount(t *testing.T, count int) {
	t.Helper()
	m.Lock()
	defer m.Unlock()
	if m.getCount != count {
		t.Errorf("expected get count %d, got %d", count, m.getCount)
	}
}

func (m *mockStorage) assertSetCount(t *testing.T, count int) {
	t.Helper()
	m.Lock()
	defer m.Unlock()
	if m.setCount != count {
		t.Errorf("expected set count %d, got %d", count, m.setCount)
	}
}

func (m *mockStorage) assertDeleteCount(t *testing.T, count int) {
	t.Helper()
	m.Lock()
	defer m.Unlock()
	if m.deleteCount != count {
		t.Errorf("expected delete count %d, got %d", count, m.deleteCount)
	}
}

func (m *mockStorage) DeleteBatch(_ context.Context, keys []string) {
	m.Lock()
	defer m.Unlock()
	for _, key := range keys {
		delete(m.records, key)
	}
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

	key := "key1"
	fetchObserver.Response(key)
	_, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecord(t, key)
	distributedStorage.assertGetCount(t, 1)
	distributedStorage.assertSetCount(t, 1)

	// Next, we'll delete the records from the in-memory cache to simulate that they were evicted.
	c.Delete(key)
	if c.Size() != 0 {
		t.Fatalf("expected cache size to be 0, got %d", c.Size())
	}

	// Now we can request the same key again. The underlying data source should not be called.
	res, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res != "valuekey1" {
		t.Errorf("expected valuekey1, got %s", res)
	}

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	fetchObserver.AssertFetchCount(t, 1)
	distributedStorage.assertGetCount(t, 2)
	distributedStorage.assertSetCount(t, 1)
	distributedStorage.assertDeleteCount(t, 0)
}

func TestDistributedStaleStorage(t *testing.T) {
	t.Parallel()

	clock := sturdyc.NewTestClock(time.Now())
	ctx := context.Background()
	ttl := time.Minute
	distributedStorage := &mockStorage{}
	c := sturdyc.New[string](1000, 10, ttl, 30,
		sturdyc.WithClock(clock),
		sturdyc.WithDistributedStaleStorage(distributedStorage, time.Minute),
	)
	fetchObserver := NewFetchObserver(1)

	key := "key1"
	fetchObserver.Response(key)
	_, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecord(t, key)
	distributedStorage.assertGetCount(t, 1)
	distributedStorage.assertSetCount(t, 1)

	// Next, we'll move the clock to make the record expire in the
	// in-memory cache, and become stale in the distributed storage.
	clock.Add(time.Minute * 2)

	// Now we can request the same key again, but we'll make the fetchFn error.
	fetchObserver.Err(errors.New("error"))
	res, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res != "valuekey1" {
		t.Errorf("expected valuekey1, got %s", res)
	}

	// We'll want to assert that the fetch observer was called again.
	time.Sleep(100 * time.Millisecond)
	fetchObserver.AssertFetchCount(t, 2)
	distributedStorage.assertGetCount(t, 2)
	distributedStorage.assertSetCount(t, 1)
	distributedStorage.assertDeleteCount(t, 0)
}

func TestDistributedStaleStorageDeletes(t *testing.T) {
	t.Parallel()

	clock := sturdyc.NewTestClock(time.Now())
	ctx := context.Background()
	ttl := time.Minute
	distributedStorage := &mockStorage{}
	c := sturdyc.New[string](1000, 10, ttl, 30,
		sturdyc.WithClock(clock),
		sturdyc.WithDistributedStaleStorage(distributedStorage, time.Minute),
	)
	fetchObserver := NewFetchObserver(1)

	key := "key1"
	fetchObserver.Response(key)
	_, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecord(t, key)
	distributedStorage.assertGetCount(t, 1)
	distributedStorage.assertSetCount(t, 1)

	// Next, we'll move the clock to make the record expire in the
	// in-memory cache, and become stale in the distributed storage.
	clock.Add(time.Minute * 2)

	// Now we can request the same key again, but we'll make the fetchFn return a
	// ErrNotFound. This should signal to the cache that the record has been
	// deleted at the underlying data source.
	fetchObserver.Err(sturdyc.ErrNotFound)
	res, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if !errors.Is(err, sturdyc.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if res != "" {
		t.Errorf("expected empty string (zero value), got %s", res)
	}

	// We'll want to assert that the fetch observer was called again.
	time.Sleep(100 * time.Millisecond)
	fetchObserver.AssertFetchCount(t, 2)
	distributedStorage.assertGetCount(t, 2)
	distributedStorage.assertSetCount(t, 1)
	distributedStorage.assertDeleteCount(t, 1)
}

func TestDistributedStaleStorageConvertsToMissingRecord(t *testing.T) {
	t.Parallel()

	clock := sturdyc.NewTestClock(time.Now())
	ctx := context.Background()
	ttl := time.Minute
	distributedStorage := &mockStorage{}
	c := sturdyc.New[string](1000, 10, ttl, 30,
		sturdyc.WithClock(clock),
		sturdyc.WithDistributedStaleStorage(distributedStorage, time.Minute),
		sturdyc.WithMissingRecordStorage(),
	)
	fetchObserver := NewFetchObserver(1)

	key := "key1"
	fetchObserver.Response(key)
	_, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecord(t, key)
	distributedStorage.assertGetCount(t, 1)
	distributedStorage.assertSetCount(t, 1)

	// Next, we'll move the clock to make the record expire in the
	// in-memory cache, and become stale in the distributed storage.
	clock.Add(time.Minute * 2)

	// Now we can request the same key again, but we'll make the fetchFn return a
	// ErrNotFound. This should signal to the cache that the record has been
	// deleted at the underlying data source.
	fetchObserver.Err(sturdyc.ErrNotFound)
	res, err := sturdyc.GetFetch(ctx, c, key, fetchObserver.Fetch)
	if !errors.Is(err, sturdyc.ErrMissingRecord) {
		t.Fatalf("expected ErrMissingRecord, got %v", err)
	}
	if res != "" {
		t.Errorf("expected empty string (zero value), got %s", res)
	}

	// We'll want to assert that the fetch observer was called again.
	time.Sleep(100 * time.Millisecond)
	fetchObserver.AssertFetchCount(t, 2)
	distributedStorage.assertGetCount(t, 2)
	distributedStorage.assertSetCount(t, 2)
	distributedStorage.assertDeleteCount(t, 0)
}

func TestDistributedStorageBatch(t *testing.T) {
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

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertRequestedRecords(t, firstBatchOfIDs)
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecords(t, firstBatchOfIDs, keyFn)
	distributedStorage.assertGetCount(t, 1)
	distributedStorage.assertSetCount(t, 1)

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

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertRequestedRecords(t, []string{"4", "5", "6"})
	fetchObserver.AssertFetchCount(t, 2)

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecords(t, secondBatchOfIDs, keyFn)
	distributedStorage.assertGetCount(t, 2)
	distributedStorage.assertSetCount(t, 2)
	distributedStorage.assertDeleteCount(t, 0)
}

func TestDistributedStaleStorageBatch(t *testing.T) {
	t.Parallel()

	clock := sturdyc.NewTestClock(time.Now())
	staleDuration := time.Minute
	ctx := context.Background()
	ttl := time.Minute
	distributedStorage := &mockStorage{}
	c := sturdyc.New[string](1000, 10, ttl, 30,
		sturdyc.WithClock(clock),
		sturdyc.WithDistributedStaleStorage(distributedStorage, staleDuration),
	)
	fetchObserver := NewFetchObserver(1)

	keyFn := c.BatchKeyFn("item")
	firstBatchOfIDs := []string{"1", "2", "3"}
	fetchObserver.BatchResponse(firstBatchOfIDs)
	_, err := sturdyc.GetFetchBatch(ctx, c, firstBatchOfIDs, keyFn, fetchObserver.FetchBatch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertRequestedRecords(t, firstBatchOfIDs)
	fetchObserver.AssertFetchCount(t, 1)
	fetchObserver.Clear()

	// The keys are written asynchonously, to the distributed storage.
	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertRecords(t, firstBatchOfIDs, keyFn)
	distributedStorage.assertGetCount(t, 1)
	distributedStorage.assertSetCount(t, 1)

	// Next, we'll delete the records from the in-memory cache to simulate that they were evicted.
	for _, id := range firstBatchOfIDs {
		c.Delete(keyFn(id))
	}
	if c.Size() != 0 {
		t.Fatalf("expected cache size to be 0, got %d", c.Size())
	}

	// Make the records stale by moving the clock, and then make the next fetch call return an error.
	clock.Add(staleDuration + 1)
	fetchObserver.Err(errors.New("error"))

	res, err := sturdyc.GetFetchBatch(ctx, c, firstBatchOfIDs, keyFn, fetchObserver.FetchBatch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for id, value := range res {
		if value != "value"+id {
			t.Errorf("expected value%s, got %s", id, value)
		}
	}

	<-fetchObserver.FetchCompleted
	fetchObserver.AssertRequestedRecords(t, firstBatchOfIDs)
	fetchObserver.AssertFetchCount(t, 2)

	time.Sleep(100 * time.Millisecond)
	distributedStorage.assertGetCount(t, 2)
	distributedStorage.assertSetCount(t, 1)
	distributedStorage.assertDeleteCount(t, 0)
}
