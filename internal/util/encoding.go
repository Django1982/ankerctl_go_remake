package util

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Enhex returns the hex encoding of the input byte slice.
func Enhex(src []byte) string {
	return hex.EncodeToString(src)
}

// Unhex returns the byte slice represented by the hex string src.
func Unhex(src string) ([]byte, error) {
	return hex.DecodeString(src)
}

// B64e returns the standard base64 encoding of the input byte slice.
func B64e(src []byte) string {
	return base64.StdEncoding.EncodeToString(src)
}

// B64d returns the byte slice represented by the base64 string src.
func B64d(src string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(src)
}

// PrettyMAC formats a 6-byte slice or 12-char hex string as a colon-separated MAC address.
func PrettyMAC(in any) string {
	var b []byte
	switch v := in.(type) {
	case []byte:
		b = v
	case string:
		var err error
		b, err = hex.DecodeString(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
	default:
		return fmt.Sprintf("%v", v)
	}

	if len(b) != 6 {
		return hex.EncodeToString(b)
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}
