package add

import "testing"

func TestAdd(t *testing.T) {
	got := Add(1, 2)
	want := 3
	if got != want {
		t.Fatalf("Add(1, 2) = %d, want %d", got, want)
	}
}
