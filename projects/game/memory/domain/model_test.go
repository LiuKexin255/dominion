package domain

import "testing"

func TestValidateMemoryID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "lower-case letters digits hyphen and underscore", id: "mem-1_ab"},
		{name: "digits only", id: "123"},
		{name: "upper-case is rejected", id: "Mem-1", wantErr: true},
		{name: "slash is rejected", id: "mem/1", wantErr: true},
		{name: "dot is rejected", id: "mem.1", wantErr: true},
		{name: "empty is rejected", id: "", wantErr: true},
		{name: "non-ascii is rejected", id: "记忆", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMemoryID(tt.id)

			// then
			assertErrorPresence(t, err, tt.wantErr)
		})
	}
}
