# Protocol Documentation

## Overview

ankerctl communicates with AnkerMake M5 printers via three protocols:

1. **MQTT** -- Cloud-based command/status channel (encrypted)
2. **PPPP** -- LAN-based P2P protocol for video and file transfer (UDP)
3. **HTTP Auth** -- Anker cloud API for login and printer discovery

## MQTT Protocol

- **Broker port**: 8789
- **Encryption**: AES-256-CBC with fixed IV `3DPrintAnkerMake`
- **Header**: 63-byte fixed structure + encrypted body + XOR checksum
- **Subscribe topic**: `/phone/maker/{SN}/notice`
- **Publish topic**: `/device/maker/{SN}/command`
- **Username**: `eufy_{user_id}`
- **Password**: user email

See [mqtt.md](mqtt.md) for message types, state machine, and packet format.

## PPPP Protocol

- **LAN discovery port**: 32108 (UDP)
- **Seed**: `EUPRAKM`
- **Transport**: Single UDP socket, 8 logical channels
- **Flow control**: DRW pipelining with 64-packet in-flight window
- **Retransmission timeout**: 0.5 seconds
- **Sequencing**: CyclicU16 (16-bit wraparound counter)

See [pppp.md](pppp.md) for channel assignments, packet types, and crypto.

## HTTP Auth Flow

1. Login via Passport API v2 (email + password)
2. Receive auth_token + ab_code (region detection)
3. Query printer list via App API v1
4. Fetch DSK keys for P2P authentication
5. Decode PPPP init strings for connection parameters

See [http-auth.md](http-auth.md) for API endpoints and data structures.

## Critical Constants

```
MQTT AES IV:     "3DPrintAnkerMake" (16 bytes)
MQTT Port:       8789
PPPP LAN Port:   32108
PPPP Seed:       "EUPRAKM"
Default Host:    127.0.0.1
Default Port:    4470
```
