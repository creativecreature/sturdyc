package sturdyc

import "errors"

var (
	// ErrStoreMissingRecord should be returned from FetchFn to indicate that we
	// want to store the record with a  cooldown. This only applies to the
	// FetchFn, for the BatchFetchFn you should enable the functionality through
	// options, and simply return a map without the missing record being present.
	ErrStoreMissingRecord = errors.New("record not found")
	// ErrMissingRecordCooldown is returned by cache.Get when a record has been fetched
	// unsuccessfully before, and is currently in cooldown.
	ErrMissingRecordCooldown = errors.New("record is currently in cooldown")
	// ErrOnlyCachedRecords is returned by cache.BatchGet when we have some of the requested
	// records in the cache, but the call to fetch the remaining records failed. The consumer
	// can then choose if they want to proceed with the cached records or retry the operation.
	ErrOnlyCachedRecords = errors.New("failed to fetch the records that we did not have cached")
)

func ErrIsStoreMissingRecordOrMissingRecordCooldown(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrStoreMissingRecord) || errors.Is(err, ErrMissingRecordCooldown)
}
