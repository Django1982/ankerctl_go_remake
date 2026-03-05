package model

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Printer holds connection and identity data for one AnkerMake printer.
type Printer struct {
	ID         string    `json:"-"`
	SN         string    `json:"-"`
	Name       string    `json:"-"`
	Model      string    `json:"-"`
	CreateTime time.Time `json:"-"`
	UpdateTime time.Time `json:"-"`
	WifiMAC    string    `json:"-"`
	IPAddr     string    `json:"-"`
	MQTTKey    []byte    `json:"-"` // 16 bytes, stored as hex in JSON
	APIHosts   string    `json:"-"`
	P2PHosts   string    `json:"-"`
	P2PDUID    string    `json:"-"`
	P2PKey     string    `json:"-"`
	P2PDID     string    `json:"-"`
}

// printerJSON is the JSON wire format for Printer.
// bytes fields are hex-encoded strings, timestamps are unix epoch floats.
type printerJSON struct {
	Type       string  `json:"__type__,omitempty"`
	ID         string  `json:"id"`
	SN         string  `json:"sn"`
	Name       string  `json:"name"`
	Model      string  `json:"model"`
	CreateTime float64 `json:"create_time"`
	UpdateTime float64 `json:"update_time"`
	WifiMAC    string  `json:"wifi_mac"`
	IPAddr     string  `json:"ip_addr"`
	MQTTKey    string  `json:"mqtt_key"`
	APIHosts   string  `json:"api_hosts"`
	P2PHosts   string  `json:"p2p_hosts"`
	P2PDUID    string  `json:"p2p_duid"`
	P2PKey     string  `json:"p2p_key"`
	P2PDID     string  `json:"p2p_did,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (p Printer) MarshalJSON() ([]byte, error) {
	return json.Marshal(printerJSON{
		Type:       "Printer",
		ID:         p.ID,
		SN:         p.SN,
		Name:       p.Name,
		Model:      p.Model,
		CreateTime: float64(p.CreateTime.Unix()) + float64(p.CreateTime.Nanosecond())/1e9,
		UpdateTime: float64(p.UpdateTime.Unix()) + float64(p.UpdateTime.Nanosecond())/1e9,
		WifiMAC:    p.WifiMAC,
		IPAddr:     p.IPAddr,
		MQTTKey:    hex.EncodeToString(p.MQTTKey),
		APIHosts:   p.APIHosts,
		P2PHosts:   p.P2PHosts,
		P2PDUID:    p.P2PDUID,
		P2PKey:     p.P2PKey,
		P2PDID:     p.P2PDID,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *Printer) UnmarshalJSON(data []byte) error {
	var raw printerJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal printer: %w", err)
	}

	mqttKey, err := hex.DecodeString(raw.MQTTKey)
	if err != nil {
		return fmt.Errorf("decode mqtt_key hex: %w", err)
	}

	p.ID = raw.ID
	p.SN = raw.SN
	p.Name = raw.Name
	p.Model = raw.Model
	p.CreateTime = timeFromUnixFloat(raw.CreateTime)
	p.UpdateTime = timeFromUnixFloat(raw.UpdateTime)
	p.WifiMAC = raw.WifiMAC
	p.IPAddr = raw.IPAddr
	p.MQTTKey = mqttKey
	p.APIHosts = raw.APIHosts
	p.P2PHosts = raw.P2PHosts
	p.P2PDUID = raw.P2PDUID
	p.P2PKey = raw.P2PKey
	p.P2PDID = raw.P2PDID

	return nil
}

// timeFromUnixFloat converts a Unix timestamp (potentially with fractional seconds)
// to a time.Time. This matches Python's datetime.fromtimestamp() behavior.
func timeFromUnixFloat(ts float64) time.Time {
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}
