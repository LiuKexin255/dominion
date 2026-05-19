package event

import (
	"errors"
	"reflect"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  Event
	}{
		{name: "basic", key: "name", value: "test", want: Event{Key: "name", Value: "test"}},
		{name: "empty", key: "", value: "", want: Event{Key: "", Value: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := String(tt.key, tt.value)
			if got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInt(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value int
		want  Event
	}{
		{name: "positive", key: "count", value: 42, want: Event{Key: "count", Value: 42}},
		{name: "negative", key: "delta", value: -1, want: Event{Key: "delta", Value: -1}},
		{name: "zero", key: "offset", value: 0, want: Event{Key: "offset", Value: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Int(tt.key, tt.value)
			if got != tt.want {
				t.Errorf("Int() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInt64(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value int64
		want  Event
	}{
		{name: "large", key: "big", value: 1 << 40, want: Event{Key: "big", Value: int64(1 << 40)}},
		{name: "zero", key: "zero", value: 0, want: Event{Key: "zero", Value: int64(0)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Int64(tt.key, tt.value)
			if got != tt.want {
				t.Errorf("Int64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value bool
		want  Event
	}{
		{name: "true", key: "enabled", value: true, want: Event{Key: "enabled", Value: true}},
		{name: "false", key: "enabled", value: false, want: Event{Key: "enabled", Value: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Bool(tt.key, tt.value)
			if got != tt.want {
				t.Errorf("Bool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAny(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
		want  Event
	}{
		{name: "slice", key: "items", value: []int{1, 2, 3}, want: Event{Key: "items", Value: []int{1, 2, 3}}},
		{name: "nil", key: "optional", value: nil, want: Event{Key: "optional", Value: nil}},
		{name: "struct", key: "point", value: struct{ X, Y int }{1, 2}, want: Event{Key: "point", Value: struct{ X, Y int }{1, 2}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Any(tt.key, tt.value)
			if got.Key != tt.want.Key || !reflect.DeepEqual(got.Value, tt.want.Value) {
				t.Errorf("Any() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErr(t *testing.T) {
	sentinel := errors.New("test error")

	tests := []struct {
		name string
		err  error
		want Event
	}{
		{name: "non-nil error", err: sentinel, want: Event{Key: "error", Value: sentinel}},
		{name: "nil error", err: nil, want: Event{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Err(tt.err)
			if got != tt.want {
				t.Errorf("Err() = %v, want %v", got, tt.want)
			}
		})
	}
}
