// Smoke test of the WebSocket transport against real gateway + loginhttp processes.
// usage: node tools/ws_smoke.mjs <login-http-base> <ws-url> [origin-less]
// Needs Node 22+ (global WebSocket and fetch). Hand-encodes the wire format on
// purpose so it does not share code with the Go implementation.
import { createHash } from "node:crypto";

const [loginBase, wsUrl] = process.argv.slice(2);
const enc = new TextEncoder(), dec = new TextDecoder();
const u16 = (n) => [(n >> 8) & 255, n & 255];
const u32 = (n) => [(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255];
const str = (s) => { const b = enc.encode(s); return [...u16(b.length), ...b]; };
const frame = (cmd, payload) => {
  const n = 2 + payload.length;
  return new Uint8Array([0, (n >> 16) & 255, (n >> 8) & 255, n & 255, ...u16(cmd), ...payload]);
};
let idx = 0;
const signed = (cmd, payload) => {
  idx++;
  const time = Math.floor(Date.now() / 1000);
  const sign = createHash("md5").update(`${idx}${time}${cmd}__sszg666__`).digest("hex");
  const pre = [...str(sign), ...u32(idx), ...u16(cmd + ((idx + 5) % 16)), ...u32(time)];
  return [frame(1197, pre), frame(cmd, payload)];
};
const fail = (m) => { console.error("FAIL:", m); process.exit(1); };

const q = new URLSearchParams({ username: "wssmoke", password: "pw123456" });
const body = await (await fetch(`${loginBase}/userlogin/userLogins2?${q}`)).text();
const token = /msg="([^"]+)"/.exec(body)?.[1] ?? fail("no token in " + body);

const ws = new WebSocket(wsUrl);
ws.binaryType = "arraybuffer";
let buf = new Uint8Array(0);
const waiters = [];
ws.onmessage = (e) => {
  const m = new Uint8Array(e.data);
  const n = new Uint8Array(buf.length + m.length); n.set(buf); n.set(m, buf.length); buf = n;
  while (buf.length >= 6) {
    const len = (buf[1] << 16) | (buf[2] << 8) | buf[3];
    if (buf.length < 4 + len) break;
    const cmd = (buf[4] << 8) | buf[5];
    const payload = buf.slice(6, 4 + len);
    buf = buf.slice(4 + len);
    waiters.splice(0).forEach((w) => w({ cmd, payload }));
    seen.push({ cmd, payload });
  }
};
const seen = [];
const next = async (cmd) => {
  for (;;) {
    const i = seen.findIndex((f) => f.cmd === cmd);
    if (i >= 0) return seen.splice(i, 1)[0];
    await Promise.race([new Promise((r) => waiters.push(r)), new Promise((_, rej) => setTimeout(() => rej(new Error("timeout waiting for " + cmd)), 4000))]);
  }
};
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = () => rej(new Error("ws error")); });
const send = (cmd, payload) => signed(cmd, payload).forEach((f) => ws.send(f));
const kv = (k, v) => [...str(k), ...str(v)];
const args = [["account", "wssmoke"], ["token", token], ["enter_srv_id", "symlf_1"], ["platform", "symlf"]];
send(1164, [...u16(args.length), ...args.flatMap(([k, v]) => kv(k, v))]);
const roles = await next(1164).catch((e) => fail(e.message));
console.log("1164 reply bytes:", roles.payload.length);
ws.close();
console.log("OK");
