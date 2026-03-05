package model

import (
	"encoding/json"
	"fmt"
)

// Account holds user authentication data.
type Account struct {
	AuthToken string `json:"-"`
	Region    string `json:"-"`
	UserID    string `json:"-"`
	Email     string `json:"-"`
	Country   string `json:"-"`
}

// accountJSON is the JSON wire format for Account.
type accountJSON struct {
	Type      string `json:"__type__,omitempty"`
	AuthToken string `json:"auth_token"`
	Region    string `json:"region"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Country   string `json:"country,omitempty"`
}

// MQTTUsername returns the MQTT username derived from the user ID.
// Format: "eufy_{user_id}"
func (a *Account) MQTTUsername() string {
	return fmt.Sprintf("eufy_%s", a.UserID)
}

// MQTTPassword returns the MQTT password (the user's email).
func (a *Account) MQTTPassword() string {
	return a.Email
}

// MarshalJSON implements json.Marshaler.
func (a Account) MarshalJSON() ([]byte, error) {
	return json.Marshal(accountJSON{
		Type:      "Account",
		AuthToken: a.AuthToken,
		Region:    a.Region,
		UserID:    a.UserID,
		Email:     a.Email,
		Country:   a.Country,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (a *Account) UnmarshalJSON(data []byte) error {
	var raw accountJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal account: %w", err)
	}

	a.AuthToken = raw.AuthToken
	a.Region = raw.Region
	a.UserID = raw.UserID
	a.Email = raw.Email
	a.Country = raw.Country

	return nil
}
