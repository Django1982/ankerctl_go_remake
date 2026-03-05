package logging

import (
	"testing"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]string // keys to check for "sha256:" prefix
	}{
		{
			name: "Basic Redaction",
			input: map[string]any{
				"auth_token": "secret_token_123",
				"commandType": 1001,
				"sn":          "ANKER123456",
			},
			expected: map[string]string{
				"auth_token": "sha256:",
				"sn":         "sha256:",
			},
		},
		{
			name: "Nested Redaction",
			input: map[string]any{
				"status": "ok",
				"payload": map[string]any{
					"mqtt_key": "my_secret_key",
					"value":    42,
				},
			},
			expected: map[string]string{
				"mqtt_key": "sha256:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redacted := Redact(tt.input)

			// Check redacted values
			for key, prefix := range tt.expected {
				var val any
				if v, ok := redacted[key]; ok {
					val = v
				} else if p, ok := redacted["payload"].(map[string]any); ok {
					val = p[key]
				}

				s, ok := val.(string)
				if !ok {
					t.Errorf("%s: expected string for key %s, got %T", tt.name, key, val)
					continue
				}
				if !startsWith(s, prefix) {
					t.Errorf("%s: key %s should start with %s, got %s", tt.name, key, prefix, s)
				}
			}

			// Check non-redacted values
			if redacted["commandType"] != 1001 && tt.name == "Basic Redaction" {
				t.Errorf("%s: commandType should be preserved, got %v", tt.name, redacted["commandType"])
			}
		})
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
