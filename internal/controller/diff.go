package controller

import (
	"reflect"
)

// IsSubsetMatch checks if all fields in 'desired' exist in 'current' with the same values.
// Fields in 'current' that are not in 'desired' are ignored.
// This enables drift detection without false positives from server-added default fields.
func IsSubsetMatch(desired, current interface{}) bool {
	if desired == nil {
		return true
	}
	if current == nil {
		return false
	}

	// Normalize numeric types for JSON comparison (JSON numbers are float64)
	desired = normalizeNumeric(desired)
	current = normalizeNumeric(current)

	switch d := desired.(type) {
	case map[string]interface{}:
		c, ok := current.(map[string]interface{})
		if !ok {
			return false
		}
		for key, dVal := range d {
			cVal, exists := c[key]
			if !exists {
				return false
			}
			if !IsSubsetMatch(dVal, cVal) {
				return false
			}
		}
		return true

	case []interface{}:
		c, ok := current.([]interface{})
		if !ok {
			return false
		}
		if len(d) != len(c) {
			return false
		}
		for i := range d {
			if !IsSubsetMatch(d[i], c[i]) {
				return false
			}
		}
		return true

	default:
		return reflect.DeepEqual(desired, current)
	}
}

// normalizeNumeric converts all integer types to float64 for consistent JSON comparison.
// JSON unmarshaling produces float64 for all numbers, but Go code may use int/int64/etc.
func normalizeNumeric(v interface{}) interface{} {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case float32:
		return float64(n)
	default:
		return v
	}
}
