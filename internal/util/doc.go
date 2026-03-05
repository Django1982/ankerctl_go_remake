// Package util provides shared utility functions for the ankerctl application.
//
// Functions:
//   - Encoding:    Hex encode/decode (enhex/unhex), base64 encode/decode
//   - Formatting:  format_duration, format_bytes, pretty_json, pretty_mac
//   - Rate limit:  Upload rate limiter (5/10/25/50/100 Mbps choices)
//   - Parsing:     JSON key=value parsing, HTTP bool parsing
//
// Python sources: libflagship/util.py, cli/util.py
package util
