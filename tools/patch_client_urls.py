#!/usr/bin/env python3
"""Point every hard-coded original-server URL in the client's Lua bytecode at the
local login service, WITHOUT recompiling any Lua.

Decompiled Lua cannot be recompiled safely (upvalues come out as globals), so
this edits the original decoded bytecode in place: the host part of each URL is
replaced by a string of exactly the same length (padding with '&' inside a
query string or '/' in the path), so no length field in the bytecode changes.
Each patched file is then repacked into a +K override with patch_lua.py.

usage: patch_client_urls.py CLIENT_ROOT [--dry-run]
  CLIENT_ROOT  the restored client tree (contains outputs/lua-bytecode)
env LUA_PATH is not needed because --bytecode-input skips compilation.
"""
import argparse, glob, os, re, subprocess, sys, tempfile

ORIGIN_IPS = [b"211.154.26.201", b"14.29.249.106", b"117.24.14.112", b"121.204.252.126"]
LOCAL = b"http://127.0.0.1:8081"
# These already have source-level overrides (config.lua); do not overwrite them.
SKIP = {"99F709C18721A583"}
URL_RE = re.compile(rb"http://(\d+\.\d+\.\d+\.\d+)(:\d+)?[ -~]*")


def loopback_same_length(n):
    """127.x.y.z spelled with zero padding so the text is n characters long."""
    extra = n - len("127.0.0.1")
    if not 0 <= extra <= 6:
        raise ValueError("cannot spell loopback in %d chars" % n)
    pads = [min(2, extra), 0, 0]
    extra -= pads[0]
    pads[1] = min(2, extra); extra -= pads[1]
    pads[2] = extra
    return ("127." + "0" * pads[0] + "0." + "0" * pads[1] + "0." + "0" * pads[2] + "1").encode()


def rewrite(url):
    m = URL_RE.match(url)
    ip, port = m.group(1), m.group(2) or b""
    if ip not in ORIGIN_IPS:
        return url
    old_prefix = b"http://" + ip + port
    rest = url[len(old_prefix):]
    pad = len(old_prefix) - len(LOCAL)
    if pad >= 0:
        if b"?" in rest:
            rest = rest.replace(b"?", b"?" + b"&" * pad, 1)
            new = LOCAL + rest
        else:
            new = LOCAL + b"/" * pad + rest
    else:  # too short for ip:8081 (a URL without an explicit port): neutralise to loopback:80
        new = b"http://" + loopback_same_length(len(ip)) + port + rest
    assert len(new) == len(url), (url, new)
    return new


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("client_root")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()
    out = os.path.join(a.client_root, "outputs")
    patch_lua = os.path.join(out, "reconstructed-client", "patch_lua.py")
    changed = 0
    for path in sorted(glob.glob(os.path.join(out, "lua-bytecode", "*.luac"))):
        h = os.path.basename(path)[:-5]
        if h in SKIP:
            continue
        data = open(path, "rb").read()
        if not any(ip in data for ip in ORIGIN_IPS):
            continue
        new = URL_RE.sub(lambda m: rewrite(m.group(0)), data)
        assert len(new) == len(data) and new != data
        for u in URL_RE.findall(new):
            assert u[0] not in ORIGIN_IPS
        changed += 1
        print(h, "->", ", ".join(sorted({m.group(0).decode()[:60] for m in URL_RE.finditer(new)})))
        if a.dry_run:
            continue
        with tempfile.NamedTemporaryFile(suffix=".luac", delete=False) as t:
            t.write(new)
        try:
            subprocess.run([sys.executable, patch_lua, "--hash", h, "--source", t.name, "--bytecode-input"],
                           check=True, stdout=subprocess.DEVNULL)
        finally:
            os.unlink(t.name)
    print("files patched:", changed)


if __name__ == "__main__":
    main()
