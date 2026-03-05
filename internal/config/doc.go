// Package config handles configuration management for ankerctl.
//
// It provides the ConfigManager for reading and writing the JSON configuration
// file (~/.config/ankerctl/default.json), and defines the data models for
// Config, Account, and Printer structs with strict Go types.
//
// Python sources: cli/config.py, cli/model.py, libflagship/logincache.py
package config
