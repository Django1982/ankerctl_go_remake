package crypto

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestEncryptLoginPasswordPubkeyFormat(t *testing.T) {
	pubkeyHex, encryptedB64, err := EncryptLoginPassword([]byte("test_password_123"))
	if err != nil {
		t.Fatalf("EncryptLoginPassword: %v", err)
	}

	// pubkeyHex must be exactly 130 hex chars: "04" (2) + X (64) + Y (64).
	if len(pubkeyHex) != 130 {
		t.Errorf("pubkeyHex length = %d, want 130", len(pubkeyHex))
	}

	// Must start with "04" (uncompressed point prefix).
	if !strings.HasPrefix(pubkeyHex, "04") {
		t.Errorf("pubkeyHex must start with '04', got prefix %q", pubkeyHex[:2])
	}

	// Must be valid hex throughout.
	decoded, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		t.Errorf("pubkeyHex is not valid hex: %v", err)
	}
	if len(decoded) != 65 {
		t.Errorf("decoded pubkey length = %d, want 65 bytes", len(decoded))
	}

	_ = encryptedB64
}

func TestEncryptLoginPasswordBase64Format(t *testing.T) {
	_, encryptedB64, err := EncryptLoginPassword([]byte("test_password_123"))
	if err != nil {
		t.Fatalf("EncryptLoginPassword: %v", err)
	}

	// Must be valid standard base64.
	decoded, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		t.Errorf("encryptedB64 is not valid standard base64: %v", err)
	}

	// AES-CBC ciphertext must be a multiple of 16 bytes.
	if len(decoded)%16 != 0 {
		t.Errorf("ciphertext length %d is not a multiple of 16", len(decoded))
	}

	// Ciphertext should be non-empty for non-empty input.
	if len(decoded) == 0 {
		t.Error("ciphertext is empty for non-empty input")
	}
}

func TestEncryptLoginPasswordEphemeralKeys(t *testing.T) {
	// Two invocations must produce different public keys because each call
	// generates a fresh ephemeral key pair.
	pubkey1, _, err := EncryptLoginPassword([]byte("password"))
	if err != nil {
		t.Fatal(err)
	}
	pubkey2, _, err := EncryptLoginPassword([]byte("password"))
	if err != nil {
		t.Fatal(err)
	}

	if pubkey1 == pubkey2 {
		t.Error("two calls to EncryptLoginPassword produced identical public keys; ephemeral keys are not random")
	}
}

func TestEncryptLoginPasswordEphemeralCiphertexts(t *testing.T) {
	// Two invocations with the same password must produce different ciphertexts
	// because the ECDH shared secret (and thus AES key) differs.
	_, ct1, err := EncryptLoginPassword([]byte("same_password"))
	if err != nil {
		t.Fatal(err)
	}
	_, ct2, err := EncryptLoginPassword([]byte("same_password"))
	if err != nil {
		t.Fatal(err)
	}

	if ct1 == ct2 {
		t.Error("two calls with the same password produced identical ciphertexts; IV is not random")
	}
}

func TestEncryptLoginPasswordEmptyPassword(t *testing.T) {
	// Empty password should still succeed — AES-CBC with PKCS7 pads to 16 bytes.
	pubkeyHex, encryptedB64, err := EncryptLoginPassword([]byte{})
	if err != nil {
		t.Fatalf("EncryptLoginPassword with empty password: %v", err)
	}

	if len(pubkeyHex) != 130 {
		t.Errorf("pubkeyHex length = %d, want 130", len(pubkeyHex))
	}

	decoded, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		t.Errorf("encryptedB64 is not valid base64: %v", err)
	}
	// Empty plaintext PKCS7-padded to 16 bytes → 16 bytes ciphertext.
	if len(decoded) != 16 {
		t.Errorf("ciphertext for empty password length = %d, want 16", len(decoded))
	}
}

func TestAnkerPublicKeyValid(t *testing.T) {
	// Verify the hardcoded Anker public key is a valid P-256 uncompressed point.
	// Using the package-level variable which is initialized by mustDecodeHex.
	if len(ankerPublicKeyBytes) != 65 {
		t.Errorf("ankerPublicKeyBytes length = %d, want 65", len(ankerPublicKeyBytes))
	}
	if ankerPublicKeyBytes[0] != 0x04 {
		t.Errorf("ankerPublicKeyBytes[0] = 0x%02x, want 0x04 (uncompressed point)", ankerPublicKeyBytes[0])
	}

	// Verify the X coordinate.
	wantX := "c5c00c4f8d1197cc7c3167c52bf7acb054d722f0ef08dcd7e0883236e0d72a38"
	gotX := hex.EncodeToString(ankerPublicKeyBytes[1:33])
	if gotX != wantX {
		t.Errorf("Anker public key X = %s, want %s", gotX, wantX)
	}

	// Verify the Y coordinate.
	wantY := "68d9750cb47fa4619248f3d83f0f662671dadc6e2d31c2f41db0161651c7c076"
	gotY := hex.EncodeToString(ankerPublicKeyBytes[33:65])
	if gotY != wantY {
		t.Errorf("Anker public key Y = %s, want %s", gotY, wantY)
	}
}

func TestEncryptLoginPasswordLongPassword(t *testing.T) {
	// Stress test with a longer password.
	longPW := []byte(strings.Repeat("x", 255))
	pubkeyHex, encryptedB64, err := EncryptLoginPassword(longPW)
	if err != nil {
		t.Fatalf("EncryptLoginPassword with long password: %v", err)
	}

	if len(pubkeyHex) != 130 {
		t.Errorf("pubkeyHex length = %d, want 130", len(pubkeyHex))
	}

	decoded, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		t.Errorf("encryptedB64 is not valid base64: %v", err)
	}

	// 255 bytes → padded to 256 bytes (multiple of 16) → 256 bytes ciphertext.
	if len(decoded) != 256 {
		t.Errorf("ciphertext for 255-byte password length = %d, want 256", len(decoded))
	}
}
