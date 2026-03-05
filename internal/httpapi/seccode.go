package httpapi

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
)

// CalcCheckCode computes the v1 check code (MD5) for a printer SN and WiFi MAC.
// Python: calc_check_code(sn, mac) -> md5("{sn}+{sn[-4:]}+{mac}")
func CalcCheckCode(sn, mac string) string {
	if len(sn) < 4 {
		return ""
	}
	input := fmt.Sprintf("%s+%s+%s", sn, sn[len(sn)-4:], mac)
	h := md5.Sum([]byte(input))
	return fmt.Sprintf("%x", h)
}

// CalHwIDSuffix sums the last 4 hex-digit values of a string.
// Python: cal_hw_id_suffix(val) -> sum of int(val[-1],16)..int(val[-4],16)
func CalHwIDSuffix(val string) int {
	if len(val) < 4 {
		return 0
	}
	total := 0
	for i := 1; i <= 4; i++ {
		c := val[len(val)-i]
		total += hexDigitValue(c)
	}
	return total
}

func hexDigitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 0
	}
}

// GenBaseCode generates the base code for check code v1.
// Python: gen_base_code(sn, mac)
func GenBaseCode(sn, mac string) string {
	if len(sn) == 0 {
		return ""
	}
	lastDigit := hexDigitValue(sn[len(sn)-1])
	offset := (lastDigit + 10) % 10
	if offset >= len(sn) {
		offset = 0
	}
	suffix := CalHwIDSuffix(mac)
	return sn[offset:] + fmt.Sprintf("%d", suffix)
}

// GenCheckCodeV1 computes the v1 security check code from base code and seed.
// Python: gen_check_code_v1(base_code, seed) — SHA-256 + shuffle algorithm.
// This must be bit-exact with the Python implementation.
func GenCheckCodeV1(baseCode, seed string) string {
	base := "01" + baseCode + seed
	sha := sha256.Sum256([]byte(base))

	// str = sha(32 bytes) + sha[10:12] (2 bytes) = 34 bytes
	str := make([]byte, 34)
	copy(str[:32], sha[:])
	copy(str[32:], sha[10:12])

	// Shuffle pass
	if str[32] < 0x7d || str[33] < 0x7d {
		str[32] = (str[32] + str[33]) & 0xFF
	}

	for x := 0; x < 32; x += 2 {
		if str[x] < 0x7d || str[x+1] < 0x7d {
			str[x] = (str[x] + str[x+1]) & 0xFF
		}
		if max(0x7d, str[x+1]) < str[x+2] {
			str[x+1] = str[x+2] - str[x+1]
		}
		if str[x+1] > 0x7d && str[x+1] > str[x+2] {
			str[x+1] = str[x+1] - str[x+2]
		}
	}

	return strings.ToUpper(hex.EncodeToString(str[0x10:0x20]))
}

func max(a, b byte) byte {
	if a > b {
		return a
	}
	return b
}

// GenRandSeed generates a random seed and sec_code for the v2 protocol.
// Python: gen_rand_seed(mac) -> (sec_ts, sec_code)
func GenRandSeed(mac string) (secTS string, secCode string) {
	rnd := rand.Intn(90000000) + 10000000

	suffix := CalHwIDSuffix(mac)
	txtBuf := fmt.Sprintf("%d%d", 1000-suffix, rnd)

	secTS = fmt.Sprintf("01%d", rnd)
	h := md5.Sum([]byte(txtBuf))
	secCode = strings.ToUpper(fmt.Sprintf("%x", h))

	return secTS, secCode
}

// CreateCheckCodeV1 creates a v1 check code for a printer SN and MAC.
// Python: create_check_code_v1(sn, mac) -> (sec_ts, sec_code)
func CreateCheckCodeV1(sn, mac string) (secTS, secCode string) {
	baseCode := GenBaseCode(sn, mac)
	secTS, seed := GenRandSeed(mac)
	secCode = GenCheckCodeV1(baseCode, seed)
	return secTS, secCode
}
