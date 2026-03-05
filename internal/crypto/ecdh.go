package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// ankerPublicKeyBytes is Anker's secp256r1 (P-256) EC public key encoded as
// an uncompressed point: 0x04 || X (32 bytes) || Y (32 bytes).
//
// X = 0xC5C00C4F8D1197CC7C3167C52BF7ACB054D722F0EF08DCD7E0883236E0D72A38
// Y = 0x68D9750CB47FA4619248F3D83F0F662671DADC6E2D31C2F41DB0161651C7C076
var ankerPublicKeyBytes = mustDecodeHex(
	"04" +
		"C5C00C4F8D1197CC7C3167C52BF7ACB054D722F0EF08DCD7E0883236E0D72A38" +
		"68D9750CB47FA4619248F3D83F0F662671DADC6E2D31C2F41DB0161651C7C076",
)

// mustDecodeHex decodes a hex string into bytes and panics if it fails.
// Used only for package-level constant initialization.
func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(fmt.Sprintf("crypto: invalid hardcoded hex constant: %v", err))
	}
	return b
}

// EncryptLoginPassword encrypts a plaintext password for the Anker login API
// using ECDH key agreement on P-256 (secp256r1).
//
// Algorithm:
//  1. Generate an ephemeral P-256 key pair.
//  2. Perform ECDH with Anker's hardcoded public key to derive a shared secret.
//     The shared secret is the X coordinate of the resulting point (32 bytes,
//     big-endian), as returned by Go's crypto/ecdh.
//  3. Use the full 32-byte shared secret as the AES-256 key.
//  4. Use the first 16 bytes of the key as the AES IV.
//  5. Encrypt the password bytes with AES-256-CBC + PKCS7.
//
// Returns:
//   - pubkeyHex: "04" + X.hex(64) + Y.hex(64) of the ephemeral public key
//   - encryptedB64: standard base64 of the AES ciphertext
func EncryptLoginPassword(password []byte) (pubkeyHex string, encryptedB64 string, err error) {
	curve := ecdh.P256()

	// Parse Anker's public key from the hardcoded uncompressed point bytes.
	ankerPub, err := curve.NewPublicKey(ankerPublicKeyBytes)
	if err != nil {
		return "", "", fmt.Errorf("crypto: parse Anker public key: %w", err)
	}

	// Generate an ephemeral key pair.
	ephemeralKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("crypto: generate ephemeral ECDH key: %w", err)
	}

	// Perform ECDH. Go's crypto/ecdh.ECDH returns the shared secret X coordinate
	// as a big-endian 32-byte slice for P-256, matching Python's secret.x.
	sharedSecret, err := ephemeralKey.ECDH(ankerPub)
	if err != nil {
		return "", "", fmt.Errorf("crypto: ECDH key agreement: %w", err)
	}

	// sharedSecret is already 32 bytes (the X coordinate), use it directly as
	// the AES-256 key.
	aesKey := sharedSecret
	aesIV := aesKey[:16]

	// Encrypt the password.
	ciphertext, err := Encrypt(password, aesKey, aesIV)
	if err != nil {
		return "", "", fmt.Errorf("crypto: encrypt password: %w", err)
	}

	// Format the ephemeral public key as an uncompressed point hex string.
	// crypto/ecdh.PublicKey.Bytes() returns the uncompressed point: 04 || X || Y.
	pubBytes := ephemeralKey.PublicKey().Bytes()
	pubkeyHex = hex.EncodeToString(pubBytes)

	encryptedB64 = base64.StdEncoding.EncodeToString(ciphertext)

	return pubkeyHex, encryptedB64, nil
}
