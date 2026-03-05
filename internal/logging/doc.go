// Package logging implements structured logging setup for the ankerctl application.
//
// It provides:
//   - SetupLogging: Initialize slog with configurable level and optional file output
//   - Named loggers: mqtt, web, history, timelapse, homeassistant (separate log files)
//   - Root logger output to both stdout and ankerctl.log (when ANKERCTL_LOG_DIR is set)
//
// Python source: cli/logfmt.py (setup_logging)
package logging
