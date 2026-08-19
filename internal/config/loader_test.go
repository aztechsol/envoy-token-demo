package config

import "testing"

func TestParseValidation(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		ok   bool
	}{
		{"valid", "version: 1\nremove_authorization_header: true\ntenants: {tenant-a: token-a}\n", true},
		{"empty map", "version: 1\ntenants: {}\n", true},
		{"omitted map", "version: 1\n", true},
		{"duplicate token", "version: 1\ntenants:\n  tenant-a: token\n  tenant-b: token\n", false},
		{"bad version", "version: 2\ntenants: {}\n", false},
		{"empty token", "version: 1\ntenants:\n  tenant: ''\n", false},
		{"empty tenant", "version: 1\ntenants:\n  '': token\n", false},
		{"uuid token and hyphenated tenant", "version: 1\ntenants:\n  tenant--dev: 550e8400-e29b-41d4-a716-446655440000\n", true},
		{"unsafe token characters", "version: 1\ntenants:\n  tenant: 'token\"x'\n", false},
		{"unsafe tenant characters", "version: 1\ntenants:\n  'tenant/dev': token\n", false},
		{"unknown field", "version: 1\nextra: true\ntenants: {}\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if (err == nil) != tt.ok {
				t.Fatalf("Parse() error = %v, want success %v", err, tt.ok)
			}
		})
	}
}
