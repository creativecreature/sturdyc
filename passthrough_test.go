package sturdyc_test

import (
	"context"
	"testing"
	"time"

	"github.com/creativecreature/sturdyc"
	"github.com/google/go-cmp/cmp"
)

func TestFullPassthrough(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 5
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New(capacity, numShards, ttl, evictionPercentage)

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
		res, err := sturdyc.Passthrough(ctx, c, id, fetchObserver.Fetch)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if res != "value1" {
			t.Errorf("expected value1, got %v", res)
		}
	}

	for i := 0; i < numPassthroughs+1; i++ {
		<-fetchObserver.FetchCompleted
	}

	fetchObserver.AssertFetchCount(t, numPassthroughs+1)
}

func TestHalfPassthrough(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 5
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New(capacity, numShards, ttl, evictionPercentage,
		sturdyc.WithStampedeProtection(time.Hour, time.Hour*2, time.Minute, true),
		sturdyc.WithPassthroughPercentage(50),
	)

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
		res, err := sturdyc.Passthrough(ctx, c, id, fetchObserver.Fetch)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if res != "value1" {
			t.Errorf("expected value1, got %v", res)
		}
	}

	// It's not possible to know how many requests we'll let through. We expect
	// half, which would be 50, but let's use 10 as a margin of safety.
	safetyMargin := 10
	for i := 0; i < (numPassthroughs/2)-safetyMargin; i++ {
		<-fetchObserver.FetchCompleted
	}

	// We'll also do a short sleep just to ensure there are no more refreshes happening.
	time.Sleep(time.Millisecond * 50)
	fetchObserver.AssertMinFetchCount(t, (numPassthroughs/2)-safetyMargin)
	fetchObserver.AssertMaxFetchCount(t, (numPassthroughs/2)+safetyMargin)
}

func TestFullPassthroughBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 5
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New(capacity, numShards, ttl, evictionPercentage)

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
		res, err := sturdyc.PassthroughBatch(ctx, c, idBatch, c.BatchKeyFn("item"), fetchObserver.FetchBatch)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !cmp.Equal(res, map[string]string{"1": "value1", "2": "value2", "3": "value3"}) {
			t.Errorf("expected value1, value2, value3, got %v", res)
		}
	}

	for i := 0; i < numPassthroughs+1; i++ {
		<-fetchObserver.FetchCompleted
	}

	fetchObserver.AssertFetchCount(t, numPassthroughs+1)
}

func TestHalfPassthroughBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	capacity := 5
	numShards := 2
	ttl := time.Minute
	evictionPercentage := 10
	c := sturdyc.New(capacity, numShards, ttl, evictionPercentage,
		sturdyc.WithStampedeProtection(time.Hour, time.Hour*2, time.Minute, true),
		sturdyc.WithPassthroughPercentage(50),
	)

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
		res, err := sturdyc.PassthroughBatch(ctx, c, idBatch, c.BatchKeyFn("item"), fetchObserver.FetchBatch)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !cmp.Equal(res, map[string]string{"1": "value1", "2": "value2", "3": "value3"}) {
			t.Errorf("expected value1, value2, value3, got %v", res)
		}
	}

	// It's not possible to know how many requests we'll let through. We expect
	// half, which would be 50, but let's use 10 as a margin of safety.
	safetyMargin := 10
	for i := 0; i < (numPassthroughs/2)-safetyMargin; i++ {
		<-fetchObserver.FetchCompleted
	}

	// We'll also do a short sleep just to ensure there are no more refreshes happening.
	time.Sleep(time.Millisecond * 50)
	fetchObserver.AssertMinFetchCount(t, (numPassthroughs/2)-safetyMargin)
	fetchObserver.AssertMaxFetchCount(t, (numPassthroughs/2)+safetyMargin)
}
