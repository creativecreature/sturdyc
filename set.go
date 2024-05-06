package sturdyc

// Set writes a single value to the cache. Returns true if it triggered an eviction.
func (c *Cache[T]) Set(key string, value T) bool {
	return c.set(key, value, false)
}

// SetMany writes multiple values to the cache. Returns true if it triggered an eviction.
func (c *Cache[T]) SetMany(records map[string]T, cacheKeyFn KeyFn) bool {
	var triggeredEviction bool
	for id, value := range records {
		evicted := c.set(cacheKeyFn(id), value, false)
		if evicted {
			triggeredEviction = true
		}
	}
	return triggeredEviction
}
