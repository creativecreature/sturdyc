package sturdyc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/creativecreature/sturdyc"
	"github.com/google/go-cmp/cmp"
)

func TestPassthrough(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 5
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New[string](capacity, numShards, ttl, evictionPercentage)

	id := "1"
	numPassthroughs := 100
	fetchObserver := NewFetchObserver(numPassthroughs + 1)
	fetchObserver.Response(id)

	res, err := sturdyc.Passthrough(ctx, c, id, fetchObserver.Fetch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res != "value1" {
		t.Errorf("expected value1, got %v", res)
	}

	for i := 0; i < numPassthroughs; i++ {
		res, passthroughErr := sturdyc.Passthrough(ctx, c, id, fetchObserver.Fetch)
		if passthroughErr != nil {
			t.Fatalf("expected no error, got %v", passthroughErr)
		}

		if res != "value1" {
			t.Errorf("expected value1, got %v", res)
		}
	}

	for i := 0; i < numPassthroughs+1; i++ {
		<-fetchObserver.FetchCompleted
	}

	fetchObserver.AssertFetchCount(t, numPassthroughs+1)

	fetchObserver.Clear()
	fetchObserver.Err(errors.New("error"))
	cachedRes, err := sturdyc.Passthrough(ctx, c, id, fetchObserver.Fetch)
	<-fetchObserver.FetchCompleted
	if err != nil {
		t.Fatal(err)
	}
	if cachedRes != "value1" {
		t.Errorf("expected value1, got %v", cachedRes)
	}
	fetchObserver.AssertFetchCount(t, numPassthroughs+2)
}

func TestPassthroughBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 5
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New[string](capacity, numShards, ttl, evictionPercentage)

	idBatch := []string{"1", "2", "3"}
	numPassthroughs := 100
	fetchObserver := NewFetchObserver(numPassthroughs + 1)
	fetchObserver.BatchResponse(idBatch)

	res, err := sturdyc.PassthroughBatch(ctx, c, idBatch, c.BatchKeyFn("item"), fetchObserver.FetchBatch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !cmp.Equal(res, map[string]string{"1": "value1", "2": "value2", "3": "value3"}) {
		t.Errorf("expected value1, value2, value3, got %v", res)
	}

	for i := 0; i < numPassthroughs; i++ {
		res, passthroughErr := sturdyc.PassthroughBatch(ctx, c, idBatch, c.BatchKeyFn("item"), fetchObserver.FetchBatch)
		if passthroughErr != nil {
			t.Fatalf("expected no error, got %v", passthroughErr)
		}
		if !cmp.Equal(res, map[string]string{"1": "value1", "2": "value2", "3": "value3"}) {
			t.Errorf("expected value1, value2, value3, got %v", res)
		}
	}

	for i := 0; i < numPassthroughs+1; i++ {
		<-fetchObserver.FetchCompleted
	}

	fetchObserver.AssertFetchCount(t, numPassthroughs+1)

	fetchObserver.Clear()
	fetchObserver.Err(errors.New("error"))
	cachedRes, err := sturdyc.PassthroughBatch(ctx, c, idBatch, c.BatchKeyFn("item"), fetchObserver.FetchBatch)
	<-fetchObserver.FetchCompleted
	if err != nil {
		t.Fatal(err)
	}
	if !cmp.Equal(cachedRes, map[string]string{"1": "value1", "2": "value2", "3": "value3"}) {
		t.Errorf("expected value1, value2, value3, got %v", cachedRes)
	}
	fetchObserver.AssertFetchCount(t, numPassthroughs+2)
}
