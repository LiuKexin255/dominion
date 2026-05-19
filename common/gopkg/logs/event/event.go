// Package event provides type-safe log event fields for structured logging.
package event

// Event represents a structured log field with a key and value.
type Event struct {
	Key   string
	Value any
}

// String creates an Event with a string value.
func String(key string, value string) Event {
	return Event{Key: key, Value: value}
}

// Int creates an Event with an int value.
func Int(key string, value int) Event {
	return Event{Key: key, Value: value}
}

// Int64 creates an Event with an int64 value.
func Int64(key string, value int64) Event {
	return Event{Key: key, Value: value}
}

// Bool creates an Event with a bool value.
func Bool(key string, value bool) Event {
	return Event{Key: key, Value: value}
}

// Any creates an Event with an arbitrary value.
func Any(key string, value any) Event {
	return Event{Key: key, Value: value}
}

// Err creates an Event for an error. If err is nil, it returns the zero value
// so that callers can safely pass error results without conditionals.
func Err(err error) Event {
	if err == nil {
		return Event{}
	}
	return Event{Key: "error", Value: err}
}
