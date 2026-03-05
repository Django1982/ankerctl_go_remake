// Package crypto implements PPPP-specific cryptographic functions.
//
// This includes the crypto_curse/decurse algorithm with its 8x8 shuffle
// table (seed: "EUPRAKM"), and the simpler encrypt/decrypt variant used
// by older protocol versions (seed: "SSD@cs2-network.").
//
// Also includes the PPPP init string decoder for extracting P2P/API
// host lists from the obfuscated connection strings in the Anker API.
//
// Python source: libflagship/megajank.py (PPPP-specific portions)
package crypto
