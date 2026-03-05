package util

import "fmt"

// FormatDuration formats seconds into HH:MM:SS.
// Returns "" for nil-like inputs, clamps negatives to 0.
func FormatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// FormatBytes formats a byte count into a human-readable string (B, KB, MB, GB, TB).
func FormatBytes(numBytes int64) string {
	if numBytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(numBytes)
	idx := 0
	for size >= 1024 && idx < len(units)-1 {
		size /= 1024
		idx++
	}
	if size >= 10 || idx == 0 {
		return fmt.Sprintf("%.0f %s", size, units[idx])
	}
	return fmt.Sprintf("%.1f %s", size, units[idx])
}
