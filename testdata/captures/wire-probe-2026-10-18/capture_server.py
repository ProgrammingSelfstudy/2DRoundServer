#!/usr/bin/env python3
"""Tolerant capture server: logs every frame as JSON; handshake is optional."""
import asyncio, json, struct, sys, time
HS = b"game_client------------"
def st(x): b = x.encode(); return struct.pack(">H", len(b)) + b
def fr(cmd, pl): return bytes([0]) + (len(pl) + 2).to_bytes(3, "big") + cmd.to_bytes(2, "big") + pl
REPLIES = [
    fr(10192, struct.pack(">B", 1) + st("ok") + struct.pack(">IH", 0x01020304, 0x0506)),
    fr(1164, struct.pack(">B", 1) + st("") + struct.pack(">H", 1)
        + struct.pack(">I", 0x0A0B0C0D) + st("a_1") + st("路飞") + struct.pack(">HBHIB", 0x0102, 1, 0x0304, 0x05060708, 1)
        + struct.pack(">B", 9)),
]
async def handle(r, w):
    log = lambda **k: print(json.dumps({"t": round(time.time(), 3), **k}, ensure_ascii=False), flush=True)
    log(event="connected", peer=w.get_extra_info("peername"))
    buf = b""
    try:
        while True:
            d = await r.read(4096)
            if not d: break
            buf += d
            if buf.startswith(HS):
                log(event="handshake"); buf = buf[len(HS):]
            while len(buf) >= 6:
                n = int.from_bytes(buf[1:4], "big"); total = 4 + n
                if n < 2 or len(buf) < total: break
                log(event="frame", flags=buf[0], cmd=int.from_bytes(buf[4:6], "big"), payload_len=n - 2, payload_hex=buf[6:total].hex())
                if int.from_bytes(buf[4:6], "big") == 1169:
                    for f in REPLIES: w.write(f)
                    await w.drain(); log(event="sent_replies", n=len(REPLIES))
                buf = buf[total:]
    finally:
        log(event="disconnected", leftover_hex=buf.hex()); w.close()
async def main():
    s = await asyncio.start_server(handle, "127.0.0.1", 8800)
    print(json.dumps({"event": "listening"}), flush=True)
    async with s: await s.serve_forever()
asyncio.run(main())
