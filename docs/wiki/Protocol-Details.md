# Protocol Details

ankerctl communicates with AnkerMake M5 printers using three protocols: MQTT (cloud commands), PPPP (LAN peer-to-peer), and HTTPS (Anker Cloud API). This page documents the protocol details relevant for development and debugging.

## MQTT Protocol

### Connection

- **Broker:** Anker's MQTT broker (region-dependent, resolved from config)
- **Port:** 8789 (TLS)
- **Client ID:** User ID from config
- **Credentials:** User ID as username, auth token as password

### Topics

| Direction | Topic Pattern | Purpose |
|-----------|--------------|---------|
| Subscribe | `/phone/maker/{SN}/notice` | Printer status events |
| Subscribe | `/phone/maker/{SN}/command/reply` | Command responses |
| Subscribe | `/phone/maker/{SN}/query/reply` | Query responses |
| Publish | `/device/maker/{SN}/command` | Send commands |
| Publish | `/device/maker/{SN}/query` | Send queries |

### Packet Structure

Each MQTT message consists of:

1. **Header** (63 bytes) -- Fixed-size, contains message type, command type, and metadata
2. **Body** -- AES-256-CBC encrypted JSON payload
3. **Checksum** -- XOR checksum appended after encryption

**Encryption:**
- Algorithm: AES-256-CBC
- Key: 32-byte key from printer config (`mqtt_key`, hex-encoded)
- IV: Fixed string `3DPrintAnkerMake` (16 bytes)
- Padding: PKCS7

### Command Types (ct values)

| ct | Name | Direction | Description |
|----|------|-----------|-------------|
| 1000 | PrintStatus | Notice | Print state machine (value: 0=idle, 1=printing, 2=paused, 8=aborted) |
| 1001 | NozzleTemp | Notice | Nozzle temperature (current/target, in 1/100 degree units) |
| 1003 | BedTemp | Notice | Bed temperature |
| 1004 | SpeedMode | Notice | Speed profile (0=normal, 1=sport, 2=ludicrous) |
| 1006 | FanSpeed | Notice | Fan speed |
| 1008 | PrintControl | Command | Control print (0=restart, 2=pause, 3=resume, 4=stop) |
| 1026 | GCodeCmd | Command | Send raw G-code |
| 1043 | QueryStatus | Query | Request current printer state |
| 1044 | FileInfo | Notice | Current print filename |
| 1052 | LayerInfo | Notice | Current layer / total layers |

### State Machine (ct=1000)

```
value=0 --> Idle / Print Complete
value=1 --> Printing
value=2 --> Paused
value=8 --> Aborted / Cancelled
```

### Progress Scale

MQTT reports progress as 0-10000. The API divides by 100 to return 0-100%.

## PPPP Protocol

### Overview

PPPP (Peer-to-Peer Protocol) is used for LAN communication with the printer. It provides file transfer, camera streaming, and remote control over a single UDP socket with 8 logical channels.

### Connection

1. **Discovery:** Broadcast UDP packet to port 32108 (LanSearch)
2. **Handshake:** PunchPkt exchange on ephemeral port
3. **Session:** Established on port 32100 for data transfer

**Key identifiers:**
- DUID: Device Unique ID (e.g., `EUPRAKM-010389-ETVLC`)
- Seed: `EUPRAKM` (derived from DUID prefix)

### Channels

| Channel | Purpose |
|---------|---------|
| 0 | Control (Xzyh frames: JSON commands, G-code responses) |
| 1 | File transfer (Aabb frames: G-code upload with CRC) |
| 2-7 | Reserved |

### Frame Types

**Xzyh** -- 16-byte header + payload, used on channel 0/1 for control messages:
- Video stream (H.264 NAL units)
- JSON command/response
- Light control

**Aabb** -- File transfer frames with CRC-32 integrity check:
- G-code file upload
- Progress callback

### DRW Pipelining

Data is sent using a sliding window protocol:
- In-flight window: max 64 packets
- Retransmission timeout: 0.5 seconds
- Sequence numbers: 16-bit wraparound (CyclicU16)

### Crypto

PPPP uses its own encryption layer separate from MQTT:
- `crypto_curse` / `crypto_decurse`: Shuffle-table based obfuscation
- `simple_encrypt` / `simple_decrypt`: XOR-based with seed `SSD@cs2-network.`
- Initstring decoder for connection parameters

## HTTP API (Anker Cloud)

### Authentication

Login uses ECDH key exchange:
1. Generate ephemeral EC keypair (secp256r1)
2. Derive shared secret using Anker's public key
3. Encrypt password with derived AES key
4. Send login request with encrypted password and public key

**Anker EC Public Key (secp256r1):**
```
X: C5C00C4F8D1197CC7C3167C52BF7ACB054D722F0EF08DCD7E0883236E0D72A38
Y: 68D9750CB47FA4619248F3D83F0F662671DADC6E2D31C2F41DB0161651C7C076
```

### API Endpoints

| Class | Purpose |
|-------|---------|
| PassportApiV1 | User profile |
| PassportApiV2 | Login (ECDH) |
| AppApiV1 | Printer list, DSK keys |
| HubApiV1/V2 | Device info, OTA, P2P connect |

### Headers

- `Gtoken`: MD5 hash of user_id
- Standard auth headers with auth token

### Region Detection

The client measures TCP connect time to multiple regional API hosts and selects the fastest one. Regions: US, EU, CN.

## Security Considerations

- MQTT keys and auth tokens must never appear in logs
- Config directory must be chmod 0700
- AES IV is fixed (protocol requirement, not a security choice)
- PPPP is LAN-only by design
- All crypto operations use constant-time comparisons where applicable
