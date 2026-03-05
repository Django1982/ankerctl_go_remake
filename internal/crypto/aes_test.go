package crypto

import (
	"bytes"
	"strings"
	"testing"
)

// knownKey is a 32-byte AES-256 key for deterministic tests.
var knownKey = []byte("0123456789abcdef0123456789abcdef")

// knownIV is a 16-byte AES IV for deterministic tests.
var knownIV = []byte("fedcba9876543210")

func TestEncryptDecryptRoundtrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"short message", []byte("hello, world")},
		{"exact block size", bytes.Repeat([]byte("a"), 16)},
		{"multiple blocks", bytes.Repeat([]byte("b"), 48)},
		{"single byte", []byte{0x42}},
		{"empty", []byte{}},
		{"binary data", []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := Encrypt(tc.plaintext, knownKey, knownIV)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}

			// Ciphertext must be a multiple of 16 bytes.
			if len(ciphertext)%16 != 0 {
				t.Errorf("ciphertext length %d is not a multiple of 16", len(ciphertext))
			}

			recovered, err := Decrypt(ciphertext, knownKey, knownIV)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}

			if !bytes.Equal(recovered, tc.plaintext) {
				t.Errorf("roundtrip mismatch: got %q, want %q", recovered, tc.plaintext)
			}
		})
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	// Different IVs must produce different ciphertexts for the same plaintext.
	pt := []byte("test plaintext")
	iv1 := []byte("1111111111111111")
	iv2 := []byte("2222222222222222")

	ct1, err := Encrypt(pt, knownKey, iv1)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := Encrypt(pt, knownKey, iv2)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Error("expected different ciphertexts for different IVs, got identical")
	}
}

func TestDecryptWrongPadding(t *testing.T) {
	// Encrypt something, then flip the last byte to corrupt padding.
	ct, err := Encrypt([]byte("hello"), knownKey, knownIV)
	if err != nil {
		t.Fatal(err)
	}
	ct[len(ct)-1] ^= 0xFF // corrupt the last padding byte

	_, err = Decrypt(ct, knownKey, knownIV)
	if err == nil {
		t.Error("expected error for corrupted padding, got nil")
	}
}

func TestDecryptEmptyCiphertext(t *testing.T) {
	_, err := Decrypt([]byte{}, knownKey, knownIV)
	if err == nil {
		t.Error("expected error for empty ciphertext, got nil")
	}
}

func TestDecryptNonBlockAlignedCiphertext(t *testing.T) {
	_, err := Decrypt([]byte{0x01, 0x02, 0x03}, knownKey, knownIV)
	if err == nil {
		t.Error("expected error for non-block-aligned ciphertext, got nil")
	}
}

func TestMQTTEncryptDecryptRoundtrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"typical MQTT payload", []byte(`{"cmd":1,"data":"test"}`)},
		{"binary payload", []byte{0x01, 0x02, 0x03, 0x04}},
		{"empty payload", []byte{}},
	}

	mqttKey := []byte("anker_test_key_1anker_test_key_1") // 32 bytes

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ct, err := MQTTEncrypt(tc.plaintext, mqttKey)
			if err != nil {
				t.Fatalf("MQTTEncrypt: %v", err)
			}

			recovered, err := MQTTDecrypt(ct, mqttKey)
			if err != nil {
				t.Fatalf("MQTTDecrypt: %v", err)
			}

			if !bytes.Equal(recovered, tc.plaintext) {
				t.Errorf("MQTT roundtrip mismatch: got %q, want %q", recovered, tc.plaintext)
			}
		})
	}
}

func TestMQTTEncryptUsesFixedIV(t *testing.T) {
	// MQTTEncrypt with the fixed IV must equal Encrypt with the fixed IV explicitly.
	mqttKey := []byte("anker_test_key_1anker_test_key_1")
	plaintext := []byte("mqtt test message")

	ct1, err := MQTTEncrypt(plaintext, mqttKey)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := Encrypt(plaintext, mqttKey, []byte(mqttAESIV))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(ct1, ct2) {
		t.Error("MQTTEncrypt does not use the fixed IV '3DPrintAnkerMake'")
	}
}

func TestMQTTFixedIVValue(t *testing.T) {
	if mqttAESIV != "3DPrintAnkerMake" {
		t.Errorf("mqttAESIV = %q, want %q", mqttAESIV, "3DPrintAnkerMake")
	}
	if len(mqttAESIV) != 16 {
		t.Errorf("mqttAESIV length = %d, want 16", len(mqttAESIV))
	}
}

func TestPKCS7Pad(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		blockSize int
		wantLen   int
		wantPad   byte
	}{
		{"empty input", []byte{}, 16, 16, 16},
		{"exact block", bytes.Repeat([]byte{0x01}, 16), 16, 32, 16},
		{"partial block", []byte{0x01, 0x02, 0x03}, 16, 16, 13},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			padded := pkcs7Pad(tc.input, tc.blockSize)
			if len(padded) != tc.wantLen {
				t.Errorf("padded length = %d, want %d", len(padded), tc.wantLen)
			}
			// Verify padding bytes.
			for i := tc.wantLen - int(tc.wantPad); i < tc.wantLen; i++ {
				if padded[i] != tc.wantPad {
					t.Errorf("padding byte at %d = 0x%02x, want 0x%02x", i, padded[i], tc.wantPad)
				}
			}
		})
	}
}

func TestPKCS7Unpad(t *testing.T) {
	t.Run("valid padding", func(t *testing.T) {
		padded := []byte{0x01, 0x02, 0x03, 0x0d, 0x0d, 0x0d, 0x0d, 0x0d,
			0x0d, 0x0d, 0x0d, 0x0d, 0x0d, 0x0d, 0x0d, 0x0d}
		got, err := pkcs7Unpad(padded, 16)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
			t.Errorf("unpad result = %v, want [1 2 3]", got)
		}
	})

	t.Run("full block padding", func(t *testing.T) {
		padded := bytes.Repeat([]byte{0x10}, 16)
		got, err := pkcs7Unpad(padded, 16)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty result, got %v", got)
		}
	})

	t.Run("zero padding value", func(t *testing.T) {
		padded := make([]byte, 16) // all zeros, padding value = 0 is invalid
		_, err := pkcs7Unpad(padded, 16)
		if err == nil {
			t.Error("expected error for zero padding value, got nil")
		}
	})

	t.Run("inconsistent padding bytes", func(t *testing.T) {
		padded := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0d, 0x0d, 0x05} // last byte says pad 5, but previous say 13
		_, err := pkcs7Unpad(padded, 16)
		if err == nil {
			t.Error("expected error for inconsistent padding, got nil")
		}
	})

	t.Run("padding value exceeds block size", func(t *testing.T) {
		padded := make([]byte, 16)
		padded[15] = 0x11 // 17 > block size 16
		_, err := pkcs7Unpad(padded, 16)
		if err == nil {
			t.Error("expected error for padding > block size, got nil")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := pkcs7Unpad([]byte{}, 16)
		if err == nil {
			t.Error("expected error for empty input, got nil")
		}
	})
}

func TestEncryptKeyTooShort(t *testing.T) {
	_, err := Encrypt([]byte("hello"), []byte("short"), []byte("1234567890123456"))
	if err == nil {
		t.Error("expected error for too-short key, got nil")
	}
	if !strings.Contains(err.Error(), "AES cipher") {
		t.Errorf("error message should mention AES cipher, got: %v", err)
	}
}
