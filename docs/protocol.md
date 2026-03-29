# ThermoPro TP25 BLE Protocol

This document describes the Bluetooth Low Energy protocol between the ThermoPro TP25 BBQ thermometer and the ESP32.

---

## Service and Characteristic UUIDs

| UUID | Role | Properties |
|------|------|------------|
| `1086fff0-3343-4817-8bb2-b32206336ce8` | Service | — |
| `1086fff1-3343-4817-8bb2-b32206336ce8` | Write characteristic | Write (commands to TP25) |
| `1086fff2-3343-4817-8bb2-b32206336ce8` | Notify characteristic | Notify (data from TP25) |

## Connection Sequence

1. **Scan** — Active BLE scan for 10 seconds, looking for advertised name `"Thermopro"` (exact match, capital T).
2. **Connect** — Create a NimBLE client and connect to the discovered device.
3. **Service discovery** — Retrieve the service by UUID, then the write and notify characteristics.
4. **Subscribe** — Register a notify callback on the notify characteristic.
5. **Handshake** — Write the following 12-byte command to the write characteristic (no response requested):

```
01 09 70 32 e2 c1 79 9d b4 d1 c7 b1
```

This initiates temperature streaming from the TP25. Without it, no notifications arrive.

## Reconnection

If the BLE connection drops, the ESP32 re-enters the scan→connect→handshake sequence with a **5-second delay** between retries. The LED turns off on disconnect and solid on when reconnected.

## Notification Payload Format

Each BLE notification is a **13-byte** frame. Messages shorter than 13 bytes or with a first byte other than `0x30` are silently discarded.

```
Offset  Bytes  Field
─────── ─────  ──────────────────────
  0       1    Header (must be 0x30)
  1       1    (reserved)
  2       1    Battery percentage (0–100)
  3       1    (reserved)
  4       1    (reserved)
  5–6     2    Probe 1 temperature (BCD-encoded Celsius)
  7–8     2    Probe 2 temperature (BCD-encoded Celsius)
  9–10    2    Probe 3 temperature (BCD-encoded Celsius)
 11–12    2    Probe 4 temperature (BCD-encoded Celsius)
```

## BCD Temperature Encoding

Each probe temperature is encoded in 2 bytes of modified Binary-Coded Decimal:

**Byte 1 (high byte):**

| Bits | Mask | Meaning |
|------|------|---------|
| 7 | `0x80` | Sign (1 = negative) |
| 6–4 | `0x70` | Hundreds digit (0–7) |
| 3–0 | `0x0F` | Tens digit (0–9) |

**Byte 2 (low byte):**

| Bits | Mask | Meaning |
|------|------|---------|
| 7–4 | `0xF0` | Ones digit (0–9) |
| 3–0 | `0x0F` | Tenths digit (0–9) |

**Decoding formula:**

```
temp = ((byte1 >> 4) & 0x07) × 100
     + (byte1 & 0x0F) × 10
     + ((byte2 >> 4) & 0x0F)
     + (byte2 & 0x0F) × 0.1

if (byte1 & 0x80):
    temp = -temp
```

Temperatures arrive in **Celsius**. The ESP32 converts to Fahrenheit before posting to the server: `temp_f = temp_c × 9/5 + 32`. Sentinel values (below) are **not** converted.

## Sentinel Values

These 2-byte patterns represent probe status rather than actual temperatures:

| Byte1 | Byte2 | Meaning | Float value used |
|-------|-------|---------|-----------------|
| `0xFF` | `0xFF` | Probe disconnected / not plugged in | `-999.0` |
| `0xDD` | `0xDD` | Probe error | `-100.0` |
| `0xEE` | `0xEE` | Probe over-temperature | `666.0` |

The sentinel float values pass through the entire pipeline unchanged (ESP32 → server → WebSocket → browser).
