package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
)

// mqttAESIV is the fixed 16-byte IV used for all MQTT message encryption.
// This value is protocol-mandated and must not change.
const mqttAESIV = "3DPrintAnkerMake"

// Encrypt encrypts plaintext using AES-256-CBC with PKCS7 padding.
// key must be 32 bytes (AES-256). iv must be 16 bytes (AES block size).
func Encrypt(plaintext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	padded, err2 := pkcs7Pad(plaintext, aes.BlockSize)
	if err2 != nil {
		return nil, fmt.Errorf("crypto: %w", err2)
	}
	ciphertext := make([]byte, len(padded))

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	return ciphertext, nil
}

// Decrypt decrypts ciphertext using AES-256-CBC and removes PKCS7 padding.
// key must be 32 bytes (AES-256). iv must be 16 bytes (AES block size).
// Returns an error if the ciphertext length is not a multiple of the block size
// or if PKCS7 unpadding fails.
func Decrypt(ciphertext, key, iv []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("crypto: ciphertext is empty")
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("crypto: ciphertext length %d is not a multiple of block size %d", len(ciphertext), aes.BlockSize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create AES cipher: %w", err)
	}

	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	unpadded, err := pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}

	return unpadded, nil
}

// MQTTEncrypt encrypts plaintext for MQTT transmission using AES-256-CBC
// with the fixed protocol IV "3DPrintAnkerMake".
func MQTTEncrypt(plaintext, key []byte) ([]byte, error) {
	return Encrypt(plaintext, key, []byte(mqttAESIV))
}

// MQTTDecrypt decrypts an MQTT payload using AES-256-CBC with the fixed
// protocol IV "3DPrintAnkerMake".
func MQTTDecrypt(ciphertext, key []byte) ([]byte, error) {
	return Decrypt(ciphertext, key, []byte(mqttAESIV))
}

// pkcs7Pad pads src to a multiple of blockSize using PKCS7.
// blockSize must be between 1 and 255.
// Returns an error if src exceeds the maximum allowed input size.
func pkcs7Pad(src []byte, blockSize int) ([]byte, error) {
	const maxPadInput = 64 * 1024 * 1024 // 64 MiB — safe upper bound for AES payloads
	if len(src) > maxPadInput {
		return nil, fmt.Errorf("pkcs7Pad: input too large: %d bytes (max %d)", len(src), maxPadInput)
	}
	padding := blockSize - (len(src) % blockSize)
	padded := make([]byte, len(src)+padding)
	copy(padded, src)
	for i := len(src); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded, nil
}

// pkcs7Unpad removes PKCS7 padding from src. Returns an error if the padding
// is invalid.
func pkcs7Unpad(src []byte, blockSize int) ([]byte, error) {
	if len(src) == 0 {
		return nil, errors.New("unpad: input is empty")
	}
	if len(src)%blockSize != 0 {
		return nil, fmt.Errorf("unpad: input length %d is not a multiple of block size %d", len(src), blockSize)
	}

	padding := int(src[len(src)-1])
	if padding == 0 || padding > blockSize {
		return nil, fmt.Errorf("unpad: invalid padding value %d", padding)
	}

	// Verify all padding bytes are equal to the padding length.
	for i := len(src) - padding; i < len(src); i++ {
		if int(src[i]) != padding {
			return nil, fmt.Errorf("unpad: inconsistent padding at byte %d", i)
		}
	}

	return src[:len(src)-padding], nil
}
