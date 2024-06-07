package main

import (
	"context"
	"log"
	"sync"
)

func newDistributedStorage() *distributedStorage {
	return &distributedStorage{
		records: make(map[string][]byte),
	}
}

type distributedStorage struct {
	sync.RWMutex
	records map[string][]byte
}

func (d *distributedStorage) Get(_ context.Context, key string) ([]byte, bool) {
	d.RLock()
	defer d.RUnlock()

	log.Printf("Getting key %s from the distributed storage\n", key)
	value, ok := d.records[key]
	return value, ok
}

func (d *distributedStorage) Set(_ context.Context, key string, value []byte) {
	d.Lock()
	defer d.Unlock()

	log.Printf("Writing key %s to the distributed storage\n", key)
	d.records[key] = value
}

func (d *distributedStorage) GetBatch(_ context.Context, keys []string) map[string][]byte {
	d.RLock()
	defer d.RUnlock()

	records := make(map[string][]byte)
	for _, key := range keys {
		log.Printf("Getting key %s from the distributed storage\n", key)
		if value, ok := d.records[key]; ok {
			records[key] = value
		}
	}
	return records
}

func (d *distributedStorage) SetBatch(_ context.Context, records map[string][]byte) {
	d.Lock()
	defer d.Unlock()

	for key, value := range records {
		log.Printf("Writing key %s to the distributed storage\n", key)
		d.records[key] = value
	}
}

func (d *distributedStorage) Delete(_ context.Context, key string) {
	d.Lock()
	defer d.Unlock()

	log.Printf("Deleting key %s from the distributed storage\n", key)
	delete(d.records, key)
}

func (d *distributedStorage) DeleteBatch(_ context.Context, keys []string) {
	d.Lock()
	defer d.Unlock()

	for _, key := range keys {
		log.Printf("Deleting key %s from the distributed storage\n", key)
		delete(d.records, key)
	}
}
