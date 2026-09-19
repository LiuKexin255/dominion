package redis

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
		{name: "instance side vector", inputs: []string{"myapp", "dev", "myredis"}, want: "k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt"},
		{name: "trimmed inputs match untrimmed", inputs: []string{" myapp ", " dev ", " myredis "}, want: "k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt"},
		{name: "default environment fallback", inputs: []string{"myapp", "default", "myredis"}, want: "FF53SVZiUNQCFp1k4B2c0fp4P4NCM729"},
		{name: "empty inputs", inputs: []string{"", "", ""}, want: "9fNf1YzJzh715ytcap6lALAFz3EbMGrD"},
		{name: "special characters", inputs: []string{"catalog", "prod", "redis@primary"}, want: "Sw5vvvmks8MXKTFgXn9jm7dcMORP2cgm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := generateStablePassword(tt.inputs...)

			// then
			if got != tt.want {
				t.Fatalf("generateStablePassword(%q) = %q, want %q", tt.inputs, got, tt.want)
			}
			if len(got) < redisPasswordMinLen {
				t.Fatalf("generateStablePassword(%q) len = %d, want >= %d", tt.inputs, len(got), redisPasswordMinLen)
			}
			if strings.Contains(got, "\x00") {
				t.Fatalf("generateStablePassword(%q) contains null byte", tt.inputs)
			}
		})
	}
}
