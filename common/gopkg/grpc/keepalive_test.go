package grpc

import "testing"

func TestWithLongLivedClientKeepalive(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("WithLongLivedClientKeepalive() panicked: %v", r)
		}
	}()

	// when
	opt := WithLongLivedClientKeepalive()

	// then
	if opt == nil {
		t.Fatal("WithLongLivedClientKeepalive() returned nil option")
	}
}

func TestWithLongLivedServerKeepalive(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("WithLongLivedServerKeepalive() panicked: %v", r)
		}
	}()

	// when
	opt := WithLongLivedServerKeepalive()

	// then
	if opt == nil {
		t.Fatal("WithLongLivedServerKeepalive() returned nil option")
	}
}
