package sturdyc

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const idPrefix = "STURDYC_ID"

func handleSlice(v reflect.Value) string {
	// If the value is a pointer to a slice, get the actual slice
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Len() < 1 {
		return "empty"
	}

	var sliceStrings []string
	for i := 0; i < v.Len(); i++ {
		sliceStrings = append(sliceStrings, fmt.Sprintf("%v", v.Index(i).Interface()))
	}

	return strings.Join(sliceStrings, ",")
}

func extractPermutation(cacheKey string) string {
	idIndex := strings.LastIndex(cacheKey, idPrefix+"-")

	// "STURDYC_ID-" not found, return the original cache key.
	if idIndex == -1 {
		return cacheKey
	}

	// Find the last "-" before "STURDYC_ID-" to ensure we include "STURDYC_ID-" in the result
	lastDashIndex := strings.LastIndex(cacheKey[:idIndex], "-")
	// "-" not found before "STURDYC_ID-", return original string
	if lastDashIndex == -1 {
		return cacheKey
	}

	return cacheKey[:lastDashIndex+1]
}

func (c *Client[T]) relativeTime(t time.Time) string {
	now := c.clock.Now().Truncate(c.keyTruncation)
	target := t.Truncate(c.keyTruncation)
	var diff time.Duration
	var direction string
	if target.After(now) {
		diff = target.Sub(now)
		direction = "(+)" // Time is in the future
	} else {
		diff = now.Sub(target)
		direction = "(-)" // Time is in the past
	}
	hours := int(diff.Hours())
	minutes := int(diff.Minutes()) % 60
	seconds := int(diff.Seconds()) % 60
	return fmt.Sprintf("%s%dh%02dm%02ds", direction, hours, minutes, seconds)
}

// handleTime turns the time.Time into an epoch string.
func (c *Client[T]) handleTime(v reflect.Value) string {
	if timestamp, ok := v.Interface().(time.Time); ok {
		if !timestamp.IsZero() {
			if c.useRelativeTimeKeyFormat {
				return c.relativeTime(timestamp)
			}
			return strconv.FormatInt(timestamp.Unix(), 10)
		}
	}

	return "empty-time"
}

func (c *Client[T]) handleStruct(permutationStruct interface{}) string {
	str := ""
	v := reflect.ValueOf(permutationStruct)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		panic("permutationStruct must be a struct")
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)

		// Skip unexported fields
		if !field.CanInterface() {
			message := fmt.Sprintf(
				"sturdyc: permutationStruct contains unexported field: %s which won't be part of the cache key",
				v.Type().Field(i).Name,
			)
			c.log.Warn(message)
			continue
		}

		if i > 0 {
			str += "-"
		}

		if field.Kind() == reflect.Ptr {
			if field.IsNil() {
				str += "nil"
				continue
			}
			// If it's not nil we'll dereference the pointer to handle its value.
			field = field.Elem()
		}

		//nolint:exhaustive // We don't need special logic for every kind.
		switch field.Kind() {
		case reflect.Slice:
			if field.IsNil() {
				str += "nil"
			} else {
				str += handleSlice(field)
			}
		case reflect.Struct:
			// Only handle time.Time structs.
			if field.Type() == reflect.TypeOf(time.Time{}) {
				str += c.handleTime(field)
			}
			continue
		// All of these types makes for bad keys.
		case reflect.Map, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
			continue
		default:
			str += fmt.Sprintf("%v", field.Interface())
		}
	}
	return str
}

// Permutated key takes a prefix, and a struct where the fields are
// concatenated with in order to make a unique cache key. Passing
// anything but a struct for "permutationStruct" will result in a panic.
//
// The cache will only use the EXPORTED fields of the struct to construct the key.
// The permutation struct should be FLAT, with no nested structs. The fields can
// be any of the basic types, as well as slices and time.Time values.
func (c *Client[T]) PermutatedKey(prefix string, permutationStruct interface{}) string {
	return prefix + "-" + c.handleStruct(permutationStruct)
}

// BatchKeyFn provides a function for that can be used in conjunction with "GetFetchBatch".
// It takes in a prefix, and returns a function that will append an ID suffix for each item.
func (c *Client[T]) BatchKeyFn(prefix string) KeyFn {
	return func(id string) string {
		return fmt.Sprintf("%s-%s-%s", prefix, idPrefix, id)
	}
}

// PermutatedBatchKeyFn provides a function that can be used in conjunction
// with GetFetchBatch. It takes a prefix, and a struct where the fields are
// concatenated with the id in order to make a unique cache key. Passing
// anything but a struct for "permutationStruct" will result in a panic.
//
// The cache will only use the EXPORTED fields of the struct to construct the key.
// The permutation struct should be FLAT, with no nested structs. The fields can
// be any of the basic types, as well as slices and time.Time values.
func (c *Client[T]) PermutatedBatchKeyFn(prefix string, permutationStruct interface{}) KeyFn {
	return func(id string) string {
		key := c.PermutatedKey(prefix, permutationStruct)
		return fmt.Sprintf("%s-%s-%s", key, idPrefix, id)
	}
}
