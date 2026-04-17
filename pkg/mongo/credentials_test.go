package mongo

import (
	"strings"
	"testing"
)

func Test_generateStablePassword(t *testing.T) {
	tests := []struct {
		name   string
		inputs []string
		want   string
	}{
		{name: "normal inputs", inputs: []string{"app", "dev", "mongo-main"}, want: "ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ"},
		{name: "trimmed inputs", inputs: []string{" grpc-hello-world ", " Dev ", " mongo-main "}, want: "20nwLsJo0pHPDaexxh7EdzwF7iV3lTKX"},
		{name: "empty inputs", inputs: []string{"", "", ""}, want: "FOa0JUWqbmTlXnNU7UpqWknphFv38Dqe"},
		{name: "special characters", inputs: []string{"catalog", "prod", "mongo@primary"}, want: "XINACh6gJae2eLOGymkK9AKHEdP7lIE7"},
		{name: "special characters with dot", inputs: []string{"deploy", "infra.deploy", "mongo"}, want: "9MCUxre8tzlVz6iGCD4rLK4Z9uMy3j0L"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := generateStablePassword(tt.inputs...)

			// then
			if got != tt.want {
				t.Fatalf("generateStablePassword(%q) = %q, want %q", tt.inputs, got, tt.want)
			}
			if len(got) < mongoPasswordMinLen {
				t.Fatalf("generateStablePassword(%q) len = %d, want >= %d", tt.inputs, len(got), mongoPasswordMinLen)
			}
			if strings.Contains(got, "\x00") {
				t.Fatalf("generateStablePassword(%q) contains null byte", tt.inputs)
			}
		})
	}
}
