package crypto

import (
	"errors"
	"fmt"
)

// XORBytes computes the XOR of all bytes in data. Returns 0 for an empty slice.
func XORBytes(data []byte) byte {
	var s byte
	for _, x := range data {
		s ^= x
	}
	return s
}

// AddChecksum appends a single XOR checksum byte to msg and returns the
// resulting slice. The checksum byte is the XOR of all bytes in msg,
// so XOR of the full returned payload equals zero.
func AddChecksum(msg []byte) []byte {
	result := make([]byte, len(msg)+1)
	copy(result, msg)
	result[len(msg)] = XORBytes(msg)
	return result
}

// RemoveChecksum validates and strips the XOR checksum byte from payload.
// It verifies that the XOR of all bytes in payload (including the checksum
// byte) equals zero, then returns payload without the last byte.
// Returns an error if the checksum is invalid.
func RemoveChecksum(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("crypto: checksum payload is empty")
	}
	if XORBytes(payload) != 0 {
		return nil, fmt.Errorf("crypto: MQTT checksum mismatch (%d bytes)", len(payload))
	}
	return payload[:len(payload)-1], nil
}
