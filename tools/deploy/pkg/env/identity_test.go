package env

import (
	"errors"
	"testing"
)

func TestValidateScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		wantErr bool
	}{
		{name: "单字符合法", scope: "a"},
		{name: "普通单词合法", scope: "alice"},
		{name: "包含数字合法", scope: "dev1"},
		{name: "最长八位合法", scope: "a1234567"},
		{name: "末尾数字合法", scope: "dev2024"},
		{name: "短名称合法", scope: "prod"},
		{name: "两位合法", scope: "ab"},
		{name: "七位合法", scope: "abc1234"},
		{name: "空字符串非法", scope: "", wantErr: true},
		{name: "大写非法", scope: "ALICE", wantErr: true},
		{name: "数字开头非法", scope: "1alice", wantErr: true},
		{name: "包含下划线非法", scope: "alice_dev", wantErr: true},
		{name: "包含连字符非法", scope: "alice-dev", wantErr: true},
		{name: "超过八位非法", scope: "a12345678", wantErr: true},
		{name: "包含点非法", scope: "alice.dev", wantErr: true},
		{name: "前导空格非法", scope: " alice", wantErr: true},
		{name: "后导空格非法", scope: "alice ", wantErr: true},
		{name: "仅数字非法", scope: "123", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given

			// when
			err := ValidateScope(tt.scope)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateScope(%q) succeeded unexpectedly", tt.scope)
				}
				if !errors.Is(err, ErrInvalidScope) {
					t.Fatalf("ValidateScope(%q) error = %v, want ErrInvalidScope", tt.scope, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateScope(%q) failed: %v", tt.scope, err)
			}
		})
	}
}

func TestValidateEnvName(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		wantErr bool
	}{
		{name: "单字符合法", envName: "b"},
		{name: "普通单词合法", envName: "dev"},
		{name: "包含数字合法", envName: "dev1"},
		{name: "最长八位合法", envName: "b1234567"},
		{name: "末尾数字合法", envName: "qa2024"},
		{name: "短名称合法", envName: "stage"},
		{name: "两位合法", envName: "qa"},
		{name: "七位合法", envName: "abc1234"},
		{name: "空字符串非法", envName: "", wantErr: true},
		{name: "大写非法", envName: "DEV", wantErr: true},
		{name: "数字开头非法", envName: "1dev", wantErr: true},
		{name: "包含下划线非法", envName: "dev_env", wantErr: true},
		{name: "包含连字符非法", envName: "dev-env", wantErr: true},
		{name: "超过八位非法", envName: "b12345678", wantErr: true},
		{name: "包含点非法", envName: "alice.dev", wantErr: true},
		{name: "前导空格非法", envName: " dev", wantErr: true},
		{name: "后导空格非法", envName: "dev ", wantErr: true},
		{name: "仅数字非法", envName: "123", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given

			// when
			err := ValidateEnvName(tt.envName)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateEnvName(%q) succeeded unexpectedly", tt.envName)
				}
				if !errors.Is(err, ErrInvalidEnvName) {
					t.Fatalf("ValidateEnvName(%q) error = %v, want ErrInvalidEnvName", tt.envName, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateEnvName(%q) failed: %v", tt.envName, err)
			}
		})
	}
}

func TestValidateFullEnvName(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "最短合法", value: "a.b"},
		{name: "常规合法", value: "alice.dev"},
		{name: "最长片段合法", value: "a1234567.b1234567"},
		{name: "空字符串非法", value: "", wantErr: true},
		{name: "缺少 scope 非法", value: "dev", wantErr: true},
		{name: "过多片段非法", value: "alice.dev.extra", wantErr: true},
		{name: "scope 大写非法", value: "ALICE.dev", wantErr: true},
		{name: "scope 数字开头非法", value: "1.dev", wantErr: true},
		{name: "空片段非法", value: "a..b", wantErr: true},
		{name: "缺少 scope 非法2", value: ".dev", wantErr: true},
		{name: "缺少环境名非法", value: "alice.", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given

			// when
			err := ValidateFullEnvName(tt.value)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate%q) succeeded unexpectedly", tt.value)
				}
				if !errors.Is(err, ErrInvalidFullEnvName) {
					t.Fatalf("Validate%q) error = %v, want ErrInvalidFullEnvName", tt.value, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate%q) failed: %v", tt.value, err)
			}
		})
	}
}

func TestParseFullEnvName(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		defaultScope string
		want         string
		wantErr      error
	}{
		{name: "输入已是完整环境名", input: "alice.dev", defaultScope: "bob", want: "alice.dev"},
		{name: "补充默认 scope", input: "dev", defaultScope: "alice", want: "alice.dev"},
		{name: "缺少默认 scope", input: "dev", wantErr: ErrNoDefaultScope},
		{name: "完整环境名片段过多", input: "alice.dev.extra", defaultScope: "bob", wantErr: ErrInvalidFullEnvName},
		{name: "完整环境名格式非法", input: "ALICE.dev", defaultScope: "bob", wantErr: ErrInvalidFullEnvName},
		{name: "空字符串非法", input: "", defaultScope: "alice", wantErr: ErrInvalidEnvName},
		{name: "最短合法完整环境名", input: "a.b", defaultScope: "alice", want: "a.b"},
		{name: "默认 scope 非法", input: "dev", defaultScope: "ALICE", wantErr: ErrInvalidScope},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given

			// when
			got, err := NewFullEnvName(tt.defaultScope, tt.input)

			// then
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("Parse%q, %q) succeeded unexpectedly", tt.input, tt.defaultScope)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Parse%q, %q) error = %v, want %v", tt.input, tt.defaultScope, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse%q, %q) failed: %v", tt.input, tt.defaultScope, err)
			}
			if string(got) != tt.want {
				t.Fatalf("Parse%q, %q) = %q, want %q", tt.input, tt.defaultScope, got, tt.want)
			}
		})
	}
}
