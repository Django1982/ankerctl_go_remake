// Package crypto implements all cryptographic operations for ankerctl.
//
// This includes:
//   - AES-256-CBC encryption/decryption for MQTT payloads (IV: "3DPrintAnkerMake")
//   - XOR checksum for MQTT message integrity
//   - ECDH (secp256r1) for Anker login password encryption
//   - PPPP crypto_curse/decurse with shuffle tables
//   - PPPP simple_encrypt/decrypt (older protocol version)
//   - PPPP init string decoder
//
// Python source: libflagship/megajank.py
package crypto
