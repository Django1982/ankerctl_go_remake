// Package model defines the core data structures for ankerctl configuration.
//
// Types:
//   - Config:   Top-level configuration with account, printers, and feature settings
//   - Account:  User authentication data (auth_token, email, user_id, region)
//   - Printer:  Printer connection data (SN, MQTT key, P2P DUID, IP address)
//
// The JSON serialization matches the Python format exactly, including the
// __type__ field for polymorphic deserialization and hex-encoded byte fields.
//
// Default configuration factories are provided for notifications, timelapse,
// and Home Assistant settings, matching the Python defaults.
//
// Python sources: cli/model.py (Config, Account, Printer, Serialize)
package model
