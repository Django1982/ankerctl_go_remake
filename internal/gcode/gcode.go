package gcode

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

var (
	estimatedTimePattern = regexp.MustCompile(`(?i);\s*estimated printing time[^=]*=\s*(.*)`) // slicer header
	timeTokenPattern     = regexp.MustCompile(`(?i)(\d+)\s*([dhms])`)
	layerCountPatterns   = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^;LAYER_COUNT:(\d+)`),
		regexp.MustCompile(`(?i)^;\s*total layer(?:s)?\s*(?:number|count)?\s*[=:]\s*(\d+)`),
	}
)

var timeUnits = map[string]int{
	"d": 86400,
	"h": 3600,
	"m": 60,
	"s": 1,
}

// PatchGCodeTime inserts a ;TIME:<seconds> marker before the first G28 if missing.
func PatchGCodeTime(data []byte) []byte {
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 {
		return data
	}

	g28Index := -1
	seconds := 0
	hasSeconds := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), ";TIME:") {
			return data
		}
		if g28Index == -1 && strings.HasPrefix(strings.ToUpper(trimmed), "G28") {
			g28Index = i
		}
		if !hasSeconds {
			if m := estimatedTimePattern.FindStringSubmatch(line); len(m) == 2 {
				if parsed, ok := parseEstimatedSeconds(m[1]); ok {
					seconds = parsed
					hasSeconds = true
				}
			}
		}
		if g28Index != -1 && hasSeconds {
			break
		}
	}

	if g28Index == -1 || !hasSeconds {
		return data
	}

	insert := ";TIME:" + strconv.Itoa(seconds) + "\n"
	patched := make([]string, 0, len(lines)+1)
	patched = append(patched, lines[:g28Index]...)
	patched = append(patched, insert)
	patched = append(patched, lines[g28Index:]...)
	return []byte(strings.Join(patched, ""))
}

// ExtractLayerCount extracts the layer count from slicer comments.
func ExtractLayerCount(data []byte) (int, bool) {
	text := string(data)
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ";") {
			break
		}
		for _, pattern := range layerCountPatterns {
			m := pattern.FindStringSubmatch(trimmed)
			if len(m) != 2 {
				continue
			}
			value, err := strconv.Atoi(m[1])
			if err == nil {
				return value, true
			}
		}
	}

	count := bytes.Count(data, []byte(";LAYER_CHANGE"))
	if count > 0 {
		return count, true
	}
	return 0, false
}

func parseEstimatedSeconds(raw string) (int, bool) {
	matches := timeTokenPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0, false
	}
	total := 0
	for _, m := range matches {
		value, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		total += value * timeUnits[strings.ToLower(m[2])]
	}
	if total <= 0 {
		return 0, false
	}
	return total, true
}
