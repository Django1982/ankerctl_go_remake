package model

import "encoding/json"

// MergeDictDefaults merges user data into a defaults map, recursively.
// Missing keys get the default value. Extra keys in data are preserved.
// This mirrors Python's merge_dict_defaults() from cli/model.py.
func MergeDictDefaults(data, defaults map[string]interface{}) map[string]interface{} {
	if data == nil {
		return defaults
	}

	merged := make(map[string]interface{}, len(defaults)+len(data))

	// Apply defaults, recursing into nested maps
	for key, defaultVal := range defaults {
		dataVal, exists := data[key]
		if !exists {
			merged[key] = defaultVal
			continue
		}

		defaultMap, defaultIsMap := defaultVal.(map[string]interface{})
		dataMap, dataIsMap := dataVal.(map[string]interface{})

		if defaultIsMap && dataIsMap {
			merged[key] = MergeDictDefaults(dataMap, defaultMap)
		} else if defaultIsMap && !dataIsMap {
			merged[key] = defaultVal
		} else {
			merged[key] = dataVal
		}
	}

	// Preserve extra keys from data not in defaults
	for key, val := range data {
		if _, exists := merged[key]; !exists {
			merged[key] = val
		}
	}

	return merged
}

// MergeJSONDefaults merges a JSON document with defaults, both as raw JSON.
// Returns the merged result as raw JSON.
func MergeJSONDefaults(data, defaults json.RawMessage) (json.RawMessage, error) {
	var dataMap, defaultsMap map[string]interface{}

	if err := json.Unmarshal(data, &dataMap); err != nil {
		return defaults, nil
	}
	if err := json.Unmarshal(defaults, &defaultsMap); err != nil {
		return data, nil
	}

	merged := MergeDictDefaults(dataMap, defaultsMap)
	return json.Marshal(merged)
}
