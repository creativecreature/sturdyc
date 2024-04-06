package sturdyc

import "time"

type entry struct {
	key                 string
	value               any
	expiresAt           time.Time
	refreshAt           time.Time
	numOfRefreshRetries int
	isMissingRecord     bool
}
