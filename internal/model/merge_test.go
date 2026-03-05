package model

import (
	"encoding/json"
	"testing"
)

func TestMergeDictDefaults_NilData_ReturnsDefaults(t *testing.T) {
	defaults := map[string]interface{}{"key1": "val1", "key2": 42}
	result := MergeDictDefaults(nil, defaults)
	if result["key1"] != "val1" {
		t.Errorf("key1 = %v, want %q", result["key1"], "val1")
	}
	if result["key2"] != 42 {
		t.Errorf("key2 = %v, want 42", result["key2"])
	}
}

func TestMergeDictDefaults_EmptyData_ReturnsDefaults(t *testing.T) {
	result := MergeDictDefaults(map[string]interface{}{}, map[string]interface{}{"a": "default-a"})
	if result["a"] != "default-a" {
		t.Errorf("a = %v, want %q", result["a"], "default-a")
	}
}

func TestMergeDictDefaults_DataOverridesDefaults(t *testing.T) {
	result := MergeDictDefaults(
		map[string]interface{}{"key": "user-value"},
		map[string]interface{}{"key": "default-value"},
	)
	if result["key"] != "user-value" {
		t.Errorf("key = %v, want %q", result["key"], "user-value")
	}
}

func TestMergeDictDefaults_ExtraKeysPreserved(t *testing.T) {
	data := map[string]interface{}{"known": "value", "extra": "extra-value", "extra2": 999}
	result := MergeDictDefaults(data, map[string]interface{}{"known": "default"})

	if result["extra"] != "extra-value" {
		t.Errorf("extra = %v, want %q", result["extra"], "extra-value")
	}
	if result["extra2"] != 999 {
		t.Errorf("extra2 = %v, want 999", result["extra2"])
	}
	if result["known"] != "value" {
		t.Errorf("known = %v, want %q", result["known"], "value")
	}
}

func TestMergeDictDefaults_NestedMaps_RecursiveMerge(t *testing.T) {
	data := map[string]interface{}{
		"nested": map[string]interface{}{"user-key": "user-val"},
	}
	defaults := map[string]interface{}{
		"nested": map[string]interface{}{
			"user-key":    "default-val",
			"default-key": "default-only",
		},
	}
	result := MergeDictDefaults(data, defaults)

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested is not a map: %T", result["nested"])
	}
	if nested["user-key"] != "user-val" {
		t.Errorf("nested.user-key = %v, want %q", nested["user-key"], "user-val")
	}
	if nested["default-key"] != "default-only" {
		t.Errorf("nested.default-key = %v, want %q", nested["default-key"], "default-only")
	}
}

func TestMergeDictDefaults_NestedTypeMismatch_DefaultWins(t *testing.T) {
	// data has scalar where default has map → default wins
	data := map[string]interface{}{"nested": "not-a-map"}
	defaults := map[string]interface{}{
		"nested": map[string]interface{}{"key": "val"},
	}
	result := MergeDictDefaults(data, defaults)

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested is not a map: %T", result["nested"])
	}
	if nested["key"] != "val" {
		t.Errorf("nested.key = %v, want %q", nested["key"], "val")
	}
}

func TestMergeDictDefaults_DeepNesting(t *testing.T) {
	data := map[string]interface{}{
		"l1": map[string]interface{}{
			"l2": map[string]interface{}{"user": "set"},
		},
	}
	defaults := map[string]interface{}{
		"l1": map[string]interface{}{
			"l2": map[string]interface{}{
				"user":    "default",
				"missing": "default-missing",
			},
		},
	}
	result := MergeDictDefaults(data, defaults)

	l1, _ := result["l1"].(map[string]interface{})
	l2, _ := l1["l2"].(map[string]interface{})
	if l2["user"] != "set" {
		t.Errorf("l1.l2.user = %v, want %q", l2["user"], "set")
	}
	if l2["missing"] != "default-missing" {
		t.Errorf("l1.l2.missing = %v, want %q", l2["missing"], "default-missing")
	}
}

func TestMergeJSONDefaults_Basic(t *testing.T) {
	merged, err := MergeJSONDefaults(
		json.RawMessage(`{"a": "user", "b": "user-b"}`),
		json.RawMessage(`{"a": "default", "c": "default-c"}`),
	)
	if err != nil {
		t.Fatalf("MergeJSONDefaults: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["a"] != "user" {
		t.Errorf("a = %v, want %q", result["a"], "user")
	}
	if result["b"] != "user-b" {
		t.Errorf("b = %v, want %q", result["b"], "user-b")
	}
	if result["c"] != "default-c" {
		t.Errorf("c = %v, want %q", result["c"], "default-c")
	}
}

func TestMergeJSONDefaults_InvalidData_ReturnsDefaults(t *testing.T) {
	merged, err := MergeJSONDefaults(json.RawMessage(`not-json`), json.RawMessage(`{"a": "default"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["a"] != "default" {
		t.Errorf("a = %v, want %q", result["a"], "default")
	}
}

func TestMergeJSONDefaults_InvalidDefaults_ReturnsData(t *testing.T) {
	merged, err := MergeJSONDefaults(json.RawMessage(`{"x": "from-data"}`), json.RawMessage(`not-json`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["x"] != "from-data" {
		t.Errorf("x = %v, want %q", result["x"], "from-data")
	}
}
