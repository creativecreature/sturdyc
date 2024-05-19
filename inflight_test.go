package sturdyc_test

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creativecreature/sturdyc"
)

func TestRequestsForMissingKeysGetDeduplicated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 100
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New[string](capacity, numShards, ttl, evictionPercentage)

	ch := make(chan string)
	var calls atomic.Int32
	fn := func(_ context.Context) (string, error) {
		calls.Add(1)
		return <-ch, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			v, err := c.GetFetch(ctx, "id-1", fn)
			if err != nil {
				t.Error(err)
			}
			if v != "value1" {
				t.Errorf("got %q; want %q", v, "value1")
			}
			wg.Done()
		}()
	}
	time.Sleep(50 * time.Millisecond)
	ch <- "value1"
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("got %d calls; wanted 1", got)
	}
}

func createBatchFn(calls *atomic.Int32, cond *sync.Cond) sturdyc.BatchFetchFn[int] {
	return func(_ context.Context, ids []string) (map[string]int, error) {
		calls.Add(1)
		vals := make(map[string]int, len(ids))
		for _, id := range ids {
			val, err := strconv.Atoi(id)
			if err != nil {
				panic(err)
			}
			vals[id] = val
		}

		cond.L.Lock()
		cond.Wait()
		cond.L.Unlock()

		return vals, nil
	}
}

func TestBatchRequestsForMissingKeysGetDeduplicated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 200
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New[int](capacity, numShards, ttl, evictionPercentage)

	// I'm going to start by creating three in-flight batches with 5 IDs each.
	var calls atomic.Int32
	cond := sync.NewCond(&sync.Mutex{})
	keyFn := c.BatchKeyFn("foo")

	go func() {
		c.GetFetchBatch(ctx, []string{"0", "1", "2", "3", "4"}, keyFn, createBatchFn(&calls, cond))
	}()
	go func() {
		c.GetFetchBatch(ctx, []string{"5", "6", "7", "8", "9"}, keyFn, createBatchFn(&calls, cond))
	}()
	go func() {
		c.GetFetchBatch(ctx, []string{"10", "11", "12", "13", "14"}, keyFn, createBatchFn(&calls, cond))
	}()

	// Now, while these batches are in-flight, I'm going to create additional requests for the same IDs in a loop.
	// On each iteration, I'm going to randomize two IDs between 1 and 15. This ensures that new requests are able
	// to pick IDs from any of the three in-flight batches and merge them.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			// We are going to get a few duplicates here too which the cache should be able to handle.
			ids := []string{strconv.Itoa(rand.IntN(14)), strconv.Itoa(rand.IntN(14))}
			res, err := c.GetFetchBatch(ctx, ids, keyFn, createBatchFn(&calls, cond))
			if err != nil {
				t.Errorf("expected no error got %v", err)
			}
			for _, id := range ids {
				val, ok := res[id]
				if !ok {
					t.Errorf("expected res to key %s", id)
				}

				intVal, err := strconv.Atoi(id)
				if err != nil {
					panic(err)
				}

				if intVal != val {
					t.Errorf("expected value %d; got %d", intVal, val)
				}
			}
			wg.Done()
		}()
	}

	time.Sleep(100 * time.Millisecond)
	cond.Broadcast()
	wg.Wait()
	if got := calls.Load(); got != 3 {
		t.Errorf("got %d calls; wanted 3", got)
	}
}
