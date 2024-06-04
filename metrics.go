package sturdyc

type MetricsRecorder interface {
	CacheHit()
	CacheMiss()
	Eviction()
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
