package main

import (
	"context"
	"log"
)

func newDistributedStorage() *distributedStorage {
	return &distributedStorage{
		records: make(map[string][]byte),
	}
}

type distributedStorage struct {
	records map[string][]byte
}

func (d *distributedStorage) Get(_ context.Context, key string) ([]byte, bool) {
	log.Printf("Getting key %s from the distributed storage\n", key)
	value, ok := d.records[key]
	return value, ok
}

func (d *distributedStorage) Set(_ context.Context, key string, value []byte) {
	log.Printf("Writing key %s to the distributed storage\n", key)
	d.records[key] = value
}

func (d *distributedStorage) GetBatch(_ context.Context, keys []string) map[string][]byte {
	records := make(map[string][]byte)
	for _, key := range keys {
		if value, ok := d.records[key]; ok {
			records[key] = value
		}
	}
	return records
}

func (d *distributedStorage) SetBatch(_ context.Context, records map[string][]byte) {
	for key, value := range records {
		d.records[key] = value
	}
}
