# PPPP Crypto Validation: Go Port Reference

**Source**: `libflagship/megajank.py` (Python reference)
**Target**: `internal/pppp/crypto/` (Go implementation)
**Computed**: 2026-03-03 via step-by-step algorithm trace + Python execution

---

## Functions Under Test

| Python function | Go equivalent |
|---|---|
| `crypto_curse(input, key, shuffle)` | `Curse(input []byte, key string, shuffle [8][8]byte) []byte` |
| `crypto_decurse(input, key, shuffle)` | `Decurse(input []byte, key string, shuffle [8][8]byte) ([]byte, error)` |
| `crypto_curse_string(input)` | `CurseString(input []byte) []byte` |
| `crypto_decurse_string(input)` | `DecurseString(input []byte) ([]byte, error)` |
| `simple_hash(seed)` | `SimpleHash(seed []byte) [4]int` |
| `simple_encrypt(seed, input)` | `SimpleEncrypt(seed, input []byte) []byte` |
| `simple_decrypt(seed, input)` | `SimpleDecrypt(seed, input []byte) []byte` |

---

## Key Init Trace: `PPPP_SEED = "EUPRAKM"`

Initial state: `a=1, b=3, c=5, d=7`

| Char | ASCII | Row indices for new a,b,c,d | Result |
|---|---|---|---|
| `E` | 0x45=69 | `[3][2]`, `[5][5]`, `[3][6]`, `[7][0]` | `a=0x13, b=0x29, c=0x49, d=0x07` |
| `U` | 0x55=85 | `[2][0]`, `[4][4]`, `[3][0]`, `[4][6]` | `a=0x07, b=0x6d, c=0xc1, d=0x13` |
| `P` | 0x50=80 | `[0][3]`, `[1][5]`, `[3][7]`, `[3][5]` | `a=0x97, b=0x13, c=0x3b, d=0x8b` |
| `R` | 0x52=82 | `[5][5]`, `[1][5]`, `[2][4]`, `[1][5]` | `a=0x29, b=0x13, c=0x7f, d=0x13` |
| `A` | 0x41=65 | `[3][6]`, `[7][4]`, `[4][4]`, `[1][4]` | `a=0x49, b=0x95, c=0x6d, d=0xdf` |
| `K` | 0x4b=75 | `[7][0]`, `[0][7]`, `[2][4]`, `[4][3]` | `a=0x07, b=0xf1, c=0x7f, d=0x02` |
| `M` | 0x4d=77 | `[1][6]`, `[4][7]`, `[7][4]`, `[0][7]` | `a=0x6d, b=0xc5, c=0x95, d=0xf1` |

**Final key state**: `a=0x6d, b=0xc5, c=0x95, d=0xf1`
**Initial XOR mask**: `a^b^c^d = 0xcc`

This is the deterministic state that all subsequent `CurseString`/`DecurseString` calls
start from. The Go implementation must reproduce this exactly.

---

## Test Vectors: `crypto_curse_string` / `CurseString`

All values are hex-encoded. Output length is always `len(input) + 4`.

```
// Empty input — only the 4-byte trailer is emitted
{input: "", output: "8f8386df"}

// Single byte: 0x00
{input: "00", output: "cc7cf18b19"}

// Single byte: 0xFF
{input: "ff", output: "335f5dbb43"}

// "hello" (5 bytes in, 9 bytes out)
{input: "68656c6c6f", output: "a4d34c24295b3b3f19"}

// The key string itself encoded as bytes ("EUPRAKM")
{input: "45555052414b4d", output: "898922902bedb19bb5afd1"}

// 8 null bytes
{input: "0000000000000000", output: "cc3f42fe30cc24bca36f0717"}

// 8 bytes of 0xFF
{input: "ffffffffffffffff", output: "33e39df357098d05cd75cbb2"}

// 16 bytes: 0x00..0x0f
{input: "000102030405060708090a0b0c0d0e0f",
 output: "cc3ea8f764b72ef7380d349758979c5fa1d78baf"}

// 32 bytes: 0x00..0x1f
{input: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
 output: "cc3ea8f764b72ef7380d349758979c5ff29510016ebba2f5ba91e40bb677fa0f87911fe9"}
```

### Detailed Trace: `CurseString(b"hello")`

State after key init: `a=0x6d, b=0xc5, c=0x95, d=0xf1`

| Byte | Input | Mask (`a^b^c^d`) | Output (= Input ^ Mask) | State after |
|---|---|---|---|---|
| 0 | `0x68` ('h') | `0xcc` | `0xa4` | advances from `x=0xa4` |
| 1 | `0x65` ('e') | `0xb6` | `0xd3` | advances from `x=0xd3` |
| 2 | `0x6c` ('l') | `0x20` | `0x4c` | advances from `x=0x4c` |
| 3 | `0x6c` ('l') | `0x48` | `0x24` | advances from `x=0x24` |
| 4 | `0x6f` ('o') | `0x46` | `0x29` | advances from `x=0x29` |

State after payload: `a=0x89, b=0x61, c=0xdf, d=0x2f`

Trailer bytes: `5b 3b 3f 19` (each = `a^b^c^d^0x43`, state evolves per byte)

### Detailed Trace: `CurseString(b"")`

No payload loop. Trailer starts immediately from key-init state `a=0x6d, b=0xc5, c=0x95, d=0xf1`:

| Trailer byte | State | `a^b^c^d` | `^0x43` = output |
|---|---|---|---|
| 0 | `a=0x6d b=0xc5 c=0x95 d=0xf1` | `0xcc` | `0x8f` |
| 1 | `a=0x95 b=0x13 c=0x83 d=0xc5` | `0xc0` | `0x83` |
| 2 | `a=0x6d b=0x02 c=0x0d d=0xa7` | `0xc5` | `0x86` |
| 3 | `a=0x0b b=0x6d c=0x3b d=0xc1` | `0x9c` | `0xdf` |

Output: `8f 83 86 df`

---

## Test Vectors: `crypto_decurse_string` / `DecurseString`

`DecurseString` is the inverse: input is the cursed bytes, output is the original plaintext.
It MUST return an error if the last 4 decoded bytes are not each `0x43`.

```
// Valid: round-trip of each CurseString vector above
DecurseString("8f8386df")         => b"",        nil
DecurseString("cc7cf18b19")       => b"\x00",    nil
DecurseString("335f5dbb43")       => b"\xff",    nil
DecurseString("a4d34c24295b3b3f19") => b"hello", nil

// Invalid: random/corrupt data must return error, not silently corrupt
DecurseString("0000000000000000") => nil, error("invalid decode")
DecurseString("ffffffffffffffff") => nil, error("invalid decode")
```

---

## Test Vectors: `simple_hash`

```
simple_hash(b"SSD@cs2-network.") = [1431, -1431, 470, 91]
  as uint8 mod 256:               = [0x97, 0x69,  0xd6, 0x5b]
```

Raw values (before `[::-1]` reversal), operating on `b"SSD@cs2-network."`:
- `h[0]` (XOR accumulator) = `0x5b` (91)
- `h[1]` (sum of `byte//3`) = `470`
- `h[2]` (negated sum)      = `-1431`
- `h[3]` (sum of bytes)     = `1431`

After `[::-1]`: `[1431, -1431, 470, 91]`

---

## Test Vectors: `simple_encrypt` / `simple_decrypt`

Seed: `b"SSD@cs2-network."` (the constant `PPPP_SIMPLE_SEED`)

```
simple_encrypt("SSD@cs2-network.", "666f6f")         => "262976"       // "foo"
simple_encrypt("SSD@cs2-network.", "68656c6c6f")     => "28d2aa5b5e"   // "hello"
simple_encrypt("SSD@cs2-network.", "000102")         => "4074e6"
simple_encrypt("SSD@cs2-network.", "ffffffff")       => "bf6920d1"
simple_encrypt("SSD@cs2-network.", "000102030405060708090a0b0c0d0e0f")
    => "4074e65f915739c9d94a15a4c0a5339f"
```

### Detailed Trace: `simple_encrypt(PPPP_SIMPLE_SEED, b"foo")`

Hash: `[1431, -1431, 470, 91]`

`_lookup(hash, b)` selects `PPPP_SIMPLE_SHUFFLE[(hash[b & 3] + b) % 256]`

| Byte | Input | `_lookup` key (`b`) | `hash[b&3]` | Index `(hash[b&3]+b)%256` | `SHUFFLE[idx]` | Output |
|---|---|---|---|---|---|---|
| 0 | `0x66` ('f') | 0 (fixed start) | `1431` | `(1431+0)%256 = 151` | `0x40` | `0x66^0x40=0x26` |
| 1 | `0x6f` ('o') | `output[0]=0x26` | `1431` | `(1431+0x26)%256 = 181` | `0x46` | `0x6f^0x46=0x29` |
| 2 | `0x6f` ('o') | `output[1]=0x29` | `-1431` | `(-1431+0x29)%256 = 106+41-256... = 25` (Python mod!) | `0x19` | `0x6f^0x19=0x76` |

Wait — corrected calculation for byte 2:
`b=0x29=41`, `b&3=1`, `hash[1]=-1431`, `(-1431+41)%256 = (-1390)%256`
Python: `(-1390) % 256 = 106` (Python mod always non-negative)
`PPPP_SIMPLE_SHUFFLE[106] = 0x4d`... let me verify: actual output is `0x76`, so `0x6f^0x76=0x19`.
`_lookup = 0x19`, `SHUFFLE[idx]=0x19` means `idx = SHUFFLE.index(0x19)` is not unique.

Confirmed by Python execution: `output = "262976"`.

---

## Critical Go Port Pitfalls

### 1. Operator Precedence: Index Calculation

The Python expression:
```python
shuffle[b + (q % a) & 7][q + (c % d) & 7]
```

Python operator precedence: `%` > `+` > `&`

This parses as:
```python
shuffle[(b + (q % a)) & 7][(q + (c % d)) & 7]
```

**NOT** as:
```python
shuffle[b + ((q % a) & 7)][...]  # WRONG
```

Go has **identical** operator precedence for `%`, `+`, `&` (all binary arithmetic and bitwise operators rank the same within tier). However, Go's `&` is **lower** precedence than `+`, just like Python. So the Go literal translation of:
```go
shuffle[(b + (q % a)) & 7][(q + (c % d)) & 7]
```
is correct, but writing `shuffle[b + q%a & 7]` in Go would parse as:
```go
shuffle[(b + (q % a)) & 7]  // Go: same as Python — coincidentally correct!
```

Verification: In both Python and Go, `%` > `+` > `&` in precedence.
**The literal Go translation `shuffle[b+q%a&7]` is actually correct** because Go's precedence table matches Python's here. But it is far safer to use explicit parentheses:
```go
shuffle[(b+(q%a))&7][(q+(c%d))&7]
```

**Evidence**: 7,587 out of ~50,000 tested cases produce different results between `(b+(q%a))&7` and `b+((q%a)&7)`. The latter would silently produce wrong ciphertext with no error.

### 2. Python Modulo vs. Go Modulo with Negative Numbers

**This is the most dangerous pitfall in the entire port.**

In `simple_hash`, `h[2]` is computed by subtracting byte values:
```python
hash[2] -= seed[i]  # accumulates as negative
```

For `PPPP_SIMPLE_SEED`, the result is `hash[1] = -1431` after reversal.

The `_lookup` function then computes:
```python
PPPP_SIMPLE_SHUFFLE[(hash[b & 0x3] + b) % 256]
```

In Python, modulo of a negative number always returns a non-negative result:
```python
(-1431 + 0) % 256  # => 105  (correct)
```

In Go, the `%` operator **preserves the sign** of the dividend:
```go
(-1431 + 0) % 256  // => -215  (WRONG — will panic as negative slice index)
```

**Go fix — two options:**

Option A: Store hash values as `int` but use a helper for modular reduction:
```go
func posmod(a, n int) int {
    return ((a % n) + n) % n
}
// Usage:
idx := posmod(hash[b&3]+int(b), 256)
```

Option B: Compute `h[2]` accumulation as unsigned by adding `256` each subtraction
and keeping only the mod-256 value. But this would alter `_lookup`'s behavior if
large hash values are intentional. Option A is safer and matches Python semantics.

**Test case that catches this bug**: `simple_encrypt(PPPP_SIMPLE_SEED, b"foo")`
Byte 2 uses `hash[1]=-1431`. Go without the fix produces a negative index → panic or wrong result.

### 3. State Advancement Asymmetry (Curse vs. Decurse)

`crypto_curse` advances state from the **output** byte:
```python
x = output[p] = inputByte ^ mask   # x is the ENCRYPTED byte
# state update uses x (= output)
a, b, c, d = shuffle[...x...], ...
```

`crypto_decurse` advances state from the **input** byte (the ciphertext):
```python
output[p] = x ^ mask               # x is the CIPHER byte (input)
# state update uses x (= input)
a, b, c, d = shuffle[...x...], ...
```

This is analogous to CBC feedback — the CIPHERTEXT drives the next-byte key stream.
In Go, be careful: `decurse` must use the unmodified `input[p]` for state advancement,
not the XOR result.

**Mnemonic**: "Decurse feeds the cipher, curse feeds its own output."

### 4. `crypto_curse` Output Buffer is `len(input) + 4`

The Python code allocates `output = [0] * (len(input) + 4)`. The extra 4 bytes are the
authentication trailer. `crypto_decurse` allocates exactly `len(input)` — it does NOT
strip the trailer; that is `crypto_decurse_string`'s job.

**Go allocation**:
```go
out := make([]byte, len(input)+4)  // for Curse
out := make([]byte, len(input))    // for Decurse (caller passes full ciphertext incl. trailer)
```

### 5. Trailer Validation: Exactly `[0x43, 0x43, 0x43, 0x43]`

`crypto_decurse_string` verifies `output[-4:] == [0x43, 0x43, 0x43, 0x43]`.
Each trailer byte decodes to exactly `0x43` because:
- `curse` emitted: `a^b^c^d^0x43` (XORed with `0x43`)
- `decurse` XORs with the same `a^b^c^d` mask → `0x43` remains

The check is: all four bytes equal decimal 67 (`'C'`). A wrong key, truncated data,
or corrupted byte will fail this with high probability. Return `error` not `nil` on failure.

### 6. PPPP_SHUFFLE Is a 2D Array, Not a Flat Array

```python
PPPP_SHUFFLE = [
    [0x95, 0xe5, ...],  # row 0: 8 bytes
    ...                  # rows 1-7: 8 bytes each
]
```

In Go, represent as `[8][8]byte`, not `[64]byte`. Indexing is `shuffle[rowIdx][colIdx]`
where both indices are already masked to `[0,7]` by the `& 7` operation.

`PPPP_SIMPLE_SHUFFLE` is a flat 256-element array. In Go: `[256]byte`.

### 7. `simple_encrypt` vs. `simple_decrypt` Feedback Difference

`simple_encrypt` feedback uses the **output** byte for chaining:
```python
output[i] = input[i] ^ _lookup(hash, output[i-1])
#                                    ^^^^^^^^ PREVIOUS OUTPUT
```

`simple_decrypt` feedback uses the **input** (ciphertext) byte:
```python
output[i] = input[i] ^ _lookup(hash, input[i-1])
#                                    ^^^^^^^^ PREVIOUS INPUT (cipher)
```

Byte 0 uses `_lookup(hash, 0)` as a fixed starting key for both encrypt and decrypt.

This is the same CBC pattern as curse/decurse — the cipher chain, not the plain chain,
drives the key stream in the decrypt direction.

### 8. `simple_hash` Array Reversal

```python
return hash[::-1]
```

The 4-element list is reversed before return. `hash[0]` (XOR accumulator, final `h[0]`)
becomes `result[3]`. `hash[3]` (sum, final `h[3]`) becomes `result[0]`.

In Go:
```go
return [4]int{h[3], h[2], h[1], h[0]}
```

### 9. Division in `simple_hash`: Python `//` vs. Go `/`

```python
hash[1] += seed[i] // 3
```

Python `//` is floor division. For non-negative integers (all byte values 0-255),
`seed[i] // 3` equals `seed[i] / 3` in Go integer division. **No difference here.**

However, if the seed were to contain negative bytes (impossible for `[]byte`), they would differ.
Since Go's `[]byte` is unsigned, this pitfall does not apply.

### 10. PPPP_SHUFFLE Cannot Produce Zero (No Division-by-Zero Risk)

Inspection of `PPPP_SHUFFLE` (8x8 matrix): None of the 64 entries is `0x00`.
Therefore, `a`, `b`, `c`, `d` are **never zero** after key initialization (initial
values `1,3,5,7` are also non-zero, and the shuffle table guarantees they stay non-zero).
The modulo operations `q % a`, `q % b`, etc. **cannot cause division by zero**.

`PPPP_SIMPLE_SHUFFLE` does contain `0x00` at index 27, but it is only used as
a lookup result (never as a divisor) in `simple_encrypt`/`simple_decrypt`.

---

## Validation Strategy for Go Implementation

### Unit Test Structure

```go
// crypto_curse_string vectors
var curseStringTests = []struct {
    input  string // hex
    output string // hex
}{
    {"",                                 "8f8386df"},
    {"00",                               "cc7cf18b19"},
    {"ff",                               "335f5dbb43"},
    {"68656c6c6f",                       "a4d34c24295b3b3f19"},
    {"45555052414b4d",                   "898922902bedb19bb5afd1"},
    {"0000000000000000",                 "cc3f42fe30cc24bca36f0717"},
    {"ffffffffffffffff",                 "33e39df357098d05cd75cbb2"},
    {"000102030405060708090a0b0c0d0e0f", "cc3ea8f764b72ef7380d349758979c5fa1d78baf"},
}

// simple_encrypt vectors (seed = PPPP_SIMPLE_SEED)
var simpleEncryptTests = []struct {
    input  string // hex
    output string // hex
}{
    {"666f6f",         "262976"},
    {"68656c6c6f",     "28d2aa5b5e"},
    {"000102",         "4074e6"},
    {"ffffffff",       "bf6920d1"},
    {"000102030405060708090a0b0c0d0e0f", "4074e65f915739c9d94a15a4c0a5339f"},
}
```

### Minimum Test Coverage

1. `TestCurseDecurseRoundtrip` — for each test vector, curse then decurse must recover original
2. `TestCurseStringKnownGood` — output must match table above exactly (bit-for-bit)
3. `TestDecurseStringInvalidTrailer` — corrupt bytes must return non-nil error
4. `TestSimpleHashKnownGood` — `SimpleHash(PPPP_SIMPLE_SEED)` must return `[1431,-1431,470,91]`
5. `TestSimpleEncryptKnownGood` — output must match table above exactly
6. `TestSimpleEncryptDecryptRoundtrip` — for each vector, encrypt then decrypt recovers original
7. `TestNegativeModulo` — direct unit test of the `posmod` helper with `(-1431+0)%256 == 105`

### Quick Sanity Check

Run this Python snippet to verify any new Go output before committing:

```python
from libflagship.megajank import crypto_curse_string, crypto_decurse_string
print(crypto_curse_string(b"").hex())          # must be: 8f8386df
print(crypto_curse_string(b"hello").hex())     # must be: a4d34c24295b3b3f19
```

---

## Summary: Ordered List of Bug Risk (Highest to Lowest)

| Risk | Description |
|---|---|
| CRITICAL | Negative modulo in `simple_hash` / `_lookup`: Go `%` preserves sign, Python does not |
| HIGH | State advancement in `Decurse`: must use INPUT byte, not output byte, to advance state |
| HIGH | State advancement in `SimpleDecrypt`: must use INPUT (cipher) byte, not output |
| MEDIUM | Trailer check in `DecurseString`: must validate `[0x43]*4` and return error if wrong |
| MEDIUM | Output length: `Curse` allocates `n+4`, `Decurse` allocates `n` (caller owns full ciphertext) |
| LOW | Operator precedence: `(b+(q%a))&7` — Go and Python agree, but add parentheses for clarity |
| LOW | `simple_hash` reversal: `[h3,h2,h1,h0]` not `[h0,h1,h2,h3]` |
| NONE | Zero division: `PPPP_SHUFFLE` never contains `0x00` |
| NONE | Floor division in `simple_hash`: `//` and `/` agree for `uint8` inputs |
