package sturdyc

// Set writes a single value to the cache. Returns true if it triggered an eviction.
func Set(c *Client, key string, value any) bool {
	return c.set(key, value, false)
}

// SetMany writes multiple values to the cache. Returns true if it triggered an eviction.
func SetMany[T any](c *Client, records map[string]T, cacheKeyFn KeyFn) bool {
	var triggeredEviction bool
	for id, value := range records {
		evicted := c.set(cacheKeyFn(id), value, false)
		if evicted {
			triggeredEviction = true
		}
	}
	return triggeredEviction
}
