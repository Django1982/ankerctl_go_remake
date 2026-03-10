package logging

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// sensitiveKeys defines a list of map keys whose values should be masked or hashed.
var sensitiveKeys = map[string]bool{
	"auth_token": true,
	"mqtt_key":   true,
	"p2p_key":    true,
	"p2p_duid":   true,
	"wifi_mac":   true,
	"sn":         true,
	"userId":     true,
	"email":      true,
	"password":   true,
	"token":      true,
	"api_key":    true,
}

// Redact returns a copy of the input map with sensitive values replaced by truncated hashes.
// This allows tracking value changes across logs without exposing the actual secrets.
func Redact(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if sensitiveKeys[strings.ToLower(k)] {
			out[k] = redactValue(v)
			continue
		}

		// Recursively redact nested maps
		if nested, ok := v.(map[string]any); ok {
			out[k] = Redact(nested)
			continue
		}

		// Handle slices of maps or values
		if slice, ok := v.([]any); ok {
			newSlice := make([]any, len(slice))
			for i, sv := range slice {
				if m, ok := sv.(map[string]any); ok {
					newSlice[i] = Redact(m)
				} else {
					newSlice[i] = sv
				}
			}
			out[k] = newSlice
			continue
		}

		out[k] = v
	}
	return out
}

// RedactID redacts a device identifier string, preserving only the last n
// characters so log entries remain useful for correlation without exposing the
// full value.  For example RedactID("EUPRAKM-010389-ETVLC", 4) returns
// "...-TVLC".  If n >= len(id) the whole value is returned unchanged.
func RedactID(id string, keepSuffix int) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "<empty>"
	}
	r := []rune(id)
	if keepSuffix <= 0 {
		return "..."
	}
	if keepSuffix >= len(r) {
		return id
	}
	return "..." + string(r[len(r)-keepSuffix:])
}

// RedactEmail redacts an email address, preserving only the first 2 characters
// of the local part and masking the rest.  For example
// RedactEmail("user@example.com") returns "us***@example.com".
// If the value is not a well-formed email it falls back to RedactID with 2
// chars preserved.
func RedactEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "<empty>"
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return RedactID(email, 2)
	}
	local := []rune(parts[0])
	const keep = 2
	if len(local) <= keep {
		return email // too short to redact meaningfully
	}
	return string(local[:keep]) + "***@" + parts[1]
}

func redactValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	s := fmt.Sprintf("%v", v)
	if s == "" {
		return "<empty>"
	}

	// Use truncated SHA256 hash for tracking
	h := sha256.New()
	h.Write([]byte(s))
	hashStr := fmt.Sprintf("%x", h.Sum(nil))

	// Format: sha256:abcd...wxyz (first 4 and last 4 of hash)
	if len(hashStr) > 12 {
		return fmt.Sprintf("sha256:%s...%s", hashStr[:4], hashStr[len(hashStr)-4:])
	}
	return "sha256:" + hashStr
}
