package crypto

import (
	"fmt"
)

// ppppSeed is the key used for the PPPP curse/decurse stream cipher.
const ppppSeed = "EUPRAKM"

// ppppShuffle is the 8×8 substitution table for the PPPP curse/decurse
// algorithm. Indices are [row][col], each value is a byte.
var ppppShuffle = [8][8]byte{
	{0x95, 0xe5, 0x61, 0x97, 0x83, 0x0d, 0xa7, 0xf1},
	{0xd3, 0x05, 0x95, 0x8b, 0xdf, 0x13, 0x6d, 0xef},
	{0x07, 0x61, 0x0d, 0x6d, 0x7f, 0x67, 0x17, 0x2b},
	{0xc1, 0xb5, 0x13, 0x0b, 0xdf, 0x8b, 0x49, 0x3b},
	{0x7f, 0x07, 0xd3, 0x02, 0x6d, 0x2f, 0x13, 0xc5},
	{0x6d, 0x3d, 0xfb, 0x0d, 0x0b, 0x29, 0xe9, 0x4f},
	{0x89, 0x2f, 0xe3, 0xe9, 0x0d, 0x83, 0x6d, 0xe5},
	{0x07, 0x53, 0x8b, 0x25, 0x95, 0x47, 0x1f, 0x29},
}

// Curse encrypts input using the PPPP stream cipher with the hardcoded seed
// "EUPRAKM" and shuffle table. The output is len(input)+4 bytes: the encrypted
// payload followed by 4 trailer bytes derived from the final cipher state XORed
// with 0x43.
func Curse(input []byte) []byte {
	return curse(input, ppppSeed, ppppShuffle)
}

// Decurse decrypts input encrypted with [Curse]. It verifies that the 4 trailer
// bytes are all 0x43 after decryption, then returns the payload without the
// trailer.
func Decurse(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, fmt.Errorf("pppp/crypto: decurse input too short (%d bytes, need at least 4)", len(input))
	}
	output := decurse(input, ppppSeed, ppppShuffle)

	// The last 4 bytes of the decrypted output must be 0x43.
	trailer := output[len(output)-4:]
	for i, b := range trailer {
		if b != 0x43 {
			return nil, fmt.Errorf("pppp/crypto: invalid decurse trailer byte %d: got 0x%02x, want 0x43", i, b)
		}
	}

	return output[:len(output)-4], nil
}

// si computes a shuffle table index. It replicates the Python expression
//
//	(offset + (x % divisor)) & 7
//
// using int arithmetic to avoid uint8 overflow that would differ from Python's
// arbitrary-precision integers. Both offset and divisor are uint8 state values
// that are always non-zero (all table entries are nonzero; initial values 1–7
// are nonzero).
func si(offset, x, divisor byte) int {
	return (int(offset) + int(x)%int(divisor)) & 7
}

// curse is the core encryption function. It returns len(input)+4 bytes.
//
// CRITICAL: The state update after encrypting each byte uses the ENCRYPTED byte
// (x after XOR), not the original plaintext byte. The 4 trailer bytes are
// produced by continuing the state machine XOR'd with 0x43.
//
// Index arithmetic note: Python's operator precedence means
//
//	b + (q % a) & 7  =>  (b + (q % a)) & 7
//
// because & has lower precedence than + and % in Python. All index computations
// are performed in int via the si() helper to avoid uint8 overflow that would
// produce different results from the Python reference.
func curse(input []byte, key string, shuffle [8][8]byte) []byte {
	// State variables match Python's initial values.
	a, b, c, d := byte(1), byte(3), byte(5), byte(7)

	// Key schedule: advance the state machine through each key byte.
	for _, q := range []byte(key) {
		na := shuffle[si(b, q, a)][si(q, c, d)]
		nb := shuffle[si(c, q, b)][si(q, d, a)]
		nc := shuffle[si(d, q, c)][si(q, a, b)]
		nd := shuffle[si(a, q, d)][si(q, b, c)]
		a, b, c, d = na, nb, nc, nd
	}

	output := make([]byte, len(input)+4)

	// Encrypt each input byte. State update uses the ciphertext byte (x after XOR).
	for p, plain := range input {
		x := plain ^ (a ^ b ^ c ^ d)
		output[p] = x
		// State update with the encrypted byte x.
		na := shuffle[si(b, x, a)][si(x, c, d)]
		nb := shuffle[si(c, x, b)][si(x, d, a)]
		nc := shuffle[si(d, x, c)][si(x, a, b)]
		nd := shuffle[si(a, x, d)][si(x, b, c)]
		a, b, c, d = na, nb, nc, nd
	}

	// Produce 4 trailer bytes. Each is (a^b^c^d) ^ 0x43 then advances the state
	// using that trailer byte.
	for p := len(input); p < len(input)+4; p++ {
		x := (a ^ b ^ c ^ d) ^ 0x43
		output[p] = x
		na := shuffle[si(b, x, a)][si(x, c, d)]
		nb := shuffle[si(c, x, b)][si(x, d, a)]
		nc := shuffle[si(d, x, c)][si(x, a, b)]
		nd := shuffle[si(a, x, d)][si(x, b, c)]
		a, b, c, d = na, nb, nc, nd
	}

	return output
}

// decurse is the core decryption function. It returns the same number of bytes
// as the input (including the 4 trailer bytes, which the caller must validate).
//
// CRITICAL: The state update after decrypting each byte uses the RAW input byte
// (before XOR), not the recovered plaintext.
func decurse(input []byte, key string, shuffle [8][8]byte) []byte {
	// State variables match Python's initial values.
	a, b, c, d := byte(1), byte(3), byte(5), byte(7)

	// Key schedule: identical to curse.
	for _, q := range []byte(key) {
		na := shuffle[si(b, q, a)][si(q, c, d)]
		nb := shuffle[si(c, q, b)][si(q, d, a)]
		nc := shuffle[si(d, q, c)][si(q, a, b)]
		nd := shuffle[si(a, q, d)][si(q, b, c)]
		a, b, c, d = na, nb, nc, nd
	}

	output := make([]byte, len(input))

	// Decrypt each byte. State update uses the RAW ciphertext byte x (the input).
	for p, x := range input {
		output[p] = x ^ (a ^ b ^ c ^ d)
		// State update with the raw ciphertext byte x.
		na := shuffle[si(b, x, a)][si(x, c, d)]
		nb := shuffle[si(c, x, b)][si(x, d, a)]
		nc := shuffle[si(d, x, c)][si(x, a, b)]
		nd := shuffle[si(a, x, d)][si(x, b, c)]
		a, b, c, d = na, nb, nc, nd
	}

	return output
}
