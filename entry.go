package sturdyc

import "time"

// entry represents a single cache entry.
type entry struct {
	key                 string
	value               any
	expiresAt           time.Time
	refreshAt           time.Time
	numOfRefreshRetries int
	isMissingRecord     bool
}
