package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccount_MQTTUsername(t *testing.T) {
	a := &Account{UserID: "12345"}
	if got := a.MQTTUsername(); got != "eufy_12345" {
		t.Errorf("MQTTUsername() = %q, want %q", got, "eufy_12345")
	}
}

func TestAccount_MQTTUsername_EmptyUserID(t *testing.T) {
	a := &Account{UserID: ""}
	if got := a.MQTTUsername(); got != "eufy_" {
		t.Errorf("MQTTUsername() = %q, want %q", got, "eufy_")
	}
}

func TestAccount_MQTTPassword_IsEmail(t *testing.T) {
	a := &Account{UserID: "user-1", Email: "printer@example.com"}
	if got := a.MQTTPassword(); got != a.Email {
		t.Errorf("MQTTPassword() = %q, want email %q", got, a.Email)
	}
}

func TestAccount_MarshalUnmarshal_Roundtrip(t *testing.T) {
	original := Account{
		AuthToken: "tok-secret-abc",
		Region:    "eu",
		UserID:    "user-9999",
		Email:     "test@example.com",
		Country:   "DE",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Account
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.AuthToken != original.AuthToken {
		t.Errorf("AuthToken = %q, want %q", restored.AuthToken, original.AuthToken)
	}
	if restored.Region != original.Region {
		t.Errorf("Region = %q, want %q", restored.Region, original.Region)
	}
	if restored.UserID != original.UserID {
		t.Errorf("UserID = %q, want %q", restored.UserID, original.UserID)
	}
	if restored.Email != original.Email {
		t.Errorf("Email = %q, want %q", restored.Email, original.Email)
	}
	if restored.Country != original.Country {
		t.Errorf("Country = %q, want %q", restored.Country, original.Country)
	}
}

func TestAccount_Marshal_TypeField(t *testing.T) {
	a := Account{AuthToken: "tok", UserID: "u1", Email: "u1@example.com"}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	typ, ok := raw["__type__"]
	if !ok {
		t.Fatal("__type__ field missing from Account JSON output")
	}
	if typ != "Account" {
		t.Errorf("__type__ = %q, want %q", typ, "Account")
	}
}

func TestAccount_Unmarshal_TypeFieldIgnored(t *testing.T) {
	jsonData := `{
		"__type__": "Account",
		"auth_token": "tok-xyz",
		"region": "us",
		"user_id": "u42",
		"email": "u42@example.com",
		"country": "US"
	}`
	var a Account
	if err := json.Unmarshal([]byte(jsonData), &a); err != nil {
		t.Fatalf("unmarshal with __type__: %v", err)
	}
	if a.UserID != "u42" {
		t.Errorf("UserID = %q, want %q", a.UserID, "u42")
	}
}

func TestAccount_Marshal_CountryOmittedWhenEmpty(t *testing.T) {
	a := Account{AuthToken: "tok", UserID: "u1", Email: "u@example.com", Country: ""}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"country"`) {
		t.Errorf("JSON contains 'country' field when it should be omitted: %s", data)
	}
}

func TestAccount_Marshal_CountryPresentWhenSet(t *testing.T) {
	a := Account{AuthToken: "tok", UserID: "u1", Email: "u@example.com", Country: "DE"}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"country"`) {
		t.Errorf("JSON is missing 'country' field: %s", data)
	}
}

func TestAccount_Unmarshal_InvalidJSON_ReturnsError(t *testing.T) {
	var a Account
	if err := json.Unmarshal([]byte(`{bad`), &a); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
