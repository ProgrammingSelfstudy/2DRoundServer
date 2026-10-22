# Wire-format probe, 2026-10-18

Device: Samsung SM-A165F, Android 16, arm64. Package `org.local.onepiece.offline`
(re-signed with a local key; manifest gained INTERNET + ACCESS_NETWORK_STATE).
The client code is recovered from a decompiled APK; this probe talks only to a
capture server on the developer's own machine (`adb reverse tcp:8800`). It never
touched the original game servers.

## Method
`probe_main.lua` replaces the package's entry script. It loads the original
`proto_mate`, starts the native `cc.SmartSocket`, connects to 127.0.0.1:8800 and
sends messages with deliberately distinctive values (`idx=0x01020304`, ...).
`capture_server.py` logs the bytes, and answers with known-value frames that the
client decodes with `GetRecv`; the decoded tables are in `probe-log.txt`.

## Confirmed (both directions, on device)
| Question | Answer | Evidence |
|---|---|---|
| Integer byte order | big-endian (u8/u16/u32, int16) | 1197: `01020304`, `0506`, `0708090a` |
| String length prefix | 2 bytes, big-endian, no NUL | 1197 `0004 41424344`; empty string `0000` |
| Array count prefix | 2 bytes, big-endian | 1164 `0002 ...`, empty `0000` |
| Field order | as in proto_mate.lua (1197 = sign, idx, code, time) | send_frames.jsonl |
| Frame header | `flags=0, len24=2+payload, cmd16` | all 6 frames |
| Heartbeat 1169 | empty payload | send_frames.jsonl |
| Server->client frames | same header; client decoded 10192 and 1164 (incl. UTF-8 name, nested array) to exactly the values sent | probe-log.txt, recv_frames.json |

Replayed by `internal/proto/capture_test.go`.

## NOT confirmed
- The 23-byte handshake `game_client------------`. The synchronous
  `SmartSocket:Connect` path used here sent **no** handshake. The async path,
  which the real login uses (`ConnectAsyn` -> `OnConnect` -> `SendHandshake`),
  returned -1 in this probe for an unknown reason, so the handshake on the real
  path was not observed on device (it comes from the Ghidra analysis only).
  The gateway should accept the handshake as optional until it is seen.
- Whether the client expects any reply to the handshake.
- The 1197 `sign` algorithm: the probe sent the placeholder "ABCD", not a real
  signature, so `md5(idx..time..cmd.."__sszg666__")` is still from the decompile.
- The real login sequence (maketoken, 1164 args, 10191/10192). The probe sent
  my own values, not what `LoginController` would send; the real game boot path
  was not run.
- Recv encoding for types not used here (int8/int32/uint64 in recv, `byte`).
