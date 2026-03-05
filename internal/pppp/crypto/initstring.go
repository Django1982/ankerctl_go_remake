package crypto

import (
	"fmt"
	"strings"
)

// ppppInitShuffle is the 54-entry substitution table for the PPPP init string
// decoder. Used to XOR-decode the obfuscated P2P/API host strings returned
// by the Anker cloud API.
var ppppInitShuffle = [54]byte{
	0x49, 0x59, 0x43, 0x3d, 0xb5, 0xbf, 0x6d, 0xa3, 0x47, 0x53,
	0x4f, 0x61, 0x65, 0xe3, 0x71, 0xe9, 0x67, 0x7f, 0x02, 0x03,
	0x0b, 0xad, 0xb3, 0x89, 0x2b, 0x2f, 0x35, 0xc1, 0x6b, 0x8b,
	0x95, 0x97, 0x11, 0xe5, 0xa7, 0x0d, 0xef, 0xf1, 0x05, 0x07,
	0x83, 0xfb, 0x9d, 0x3b, 0xc5, 0xc7, 0x13, 0x17, 0x1d, 0x1f,
	0x25, 0x29, 0xd3, 0xdf,
}

// DecodeInitStringRaw decodes a raw byte sequence from the PPPP init string
// format. The input must have an even number of bytes; each pair of bytes
// encodes one output byte.
//
// Encoding: each byte is represented as two ASCII chars where each char has
// 0x41 added to a 4-bit nibble. The high nibble is first.
//
// Decoding is stateful: each output byte depends on all previously decoded
// bytes via cumulative XOR. The shuffle table provides an additional per-index
// XOR constant.
func DecodeInitStringRaw(input []byte) []byte {
	olen := len(input) >> 1
	output := make([]byte, olen)

	for q := 0; q < olen; q++ {
		// Base XOR: 0x39 ^ shuffle[q % 54]
		xorVal := byte(0x39) ^ ppppInitShuffle[q%54]

		// Fold in all previously decoded output bytes.
		for p := 0; p < q; p++ {
			xorVal ^= output[p]
		}

		// Decode the two encoded nibbles from the input pair.
		h := input[q*2+0] - 0x41 // high nibble
		l := input[q*2+1] - 0x41 // low nibble

		output[q] = xorVal ^ (l + (h << 4))
	}

	return output
}

// DecodeInitString decodes a PPPP init string (provided as a Go string) and
// returns the comma-separated host/address fields it contains.
//
// The input is the obfuscated string returned by the Anker cloud API. After
// decoding, any trailing commas are stripped and the result is split on commas.
func DecodeInitString(input string) ([]string, error) {
	raw := DecodeInitStringRaw([]byte(input))

	decoded := string(raw)
	decoded = strings.TrimRight(decoded, ",")

	if decoded == "" {
		return nil, fmt.Errorf("pppp/crypto: DecodeInitString: decoded empty string from %d-byte input", len(input))
	}

	return strings.Split(decoded, ","), nil
}
