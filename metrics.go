package sturdyc

type MetricsRecorder interface {
	CacheHit()
	CacheMiss()
	ForcedEviction()
	EntriesEvicted(int)
	ShardIndex(int)
	CacheBatchRefreshSize(size int)
	ObserveCacheSize(callback func() int)
}

type DistributedMetricsRecorder interface {
	MetricsRecorder
	DistributedCacheHit()
	DistributedCacheMiss()
}

type DistributedStaleMetricsRecorder interface {
	DistributedMetricsRecorder
	DistributedStaleFallback()
}

type distributedMetricsRecorder struct {
	DistributedMetricsRecorder
}

func (d *distributedMetricsRecorder) DistributedStaleFallback() {}

func (s *shard[T]) reportForcedEviction() {
	if s.metricsRecorder == nil {
		return
	}
	s.metricsRecorder.ForcedEviction()
}

func (s *shard[T]) reportEntriesEvicted(n int) {
	if s.metricsRecorder == nil {
		return
	}
	s.metricsRecorder.EntriesEvicted(n)
}

// reportCacheHits is used to report cache hits and misses to the metrics recorder.
func (c *Client[T]) reportCacheHits(cacheHit bool) {
	if c.metricsRecorder == nil {
		return
	}
	if !cacheHit {
		c.metricsRecorder.CacheMiss()
		return
	}
	c.metricsRecorder.CacheHit()
}

func (c *Client[T]) reportShardIndex(index int) {
	if c.metricsRecorder == nil {
		return
	}
	c.metricsRecorder.ShardIndex(index)
}

func (c *Client[T]) reportBatchRefreshSize(n int) {
	if c.metricsRecorder == nil {
		return
	}
	c.metricsRecorder.CacheBatchRefreshSize(n)
}

func (c *Client[T]) reportDistributedCacheHit(cacheHit bool) {
	if c.distributedMetricsRecorder == nil {
		return
	}
	if !cacheHit {
		c.distributedMetricsRecorder.DistributedCacheMiss()
		return
	}
	c.distributedMetricsRecorder.DistributedCacheHit()
}

func (c *Client[T]) reportDistributedStaleFallback() {
	if c.distributedMetricsRecorder == nil {
		return
	}
	c.distributedMetricsRecorder.DistributedStaleFallback()
}
