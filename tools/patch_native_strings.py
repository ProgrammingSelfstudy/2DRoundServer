#!/usr/bin/env python3
"""Binary-patch external host strings in the original libcocos2dlua.so so the
rebuilt client can run with no outside network.

Each target is a NUL-terminated C string literal in .rodata; it is replaced by
a shorter-or-equal string and NUL padded, so no offset changes. Exactly one
occurrence of every pattern is required. The original library is only read;
the patched copy goes where the Apktool project keeps its lib/ directory.

usage: patch_native_strings.py CLIENT_ROOT
"""
import hashlib, os, sys

REPLACEMENTS = [
    # XMLHttpRequest reachability probe (native code tests this before every XHR)
    (b"http://www.baidu.com", b"http://127.1:8081"),
    (b"http://ff.y17.xyz", b"http://127.1:8081"),
    # bare host literals found next to CCGameMain init
    (b"103.217.200.127", b"127.0.0.1"),
    (b"211.154.26.201", b"127.0.0.1"),
]


def main():
    root = sys.argv[1]
    src = os.path.join(root, "outputs/apk-extracted/lib/arm64-v8a/libcocos2dlua.so")
    dst = os.path.join(root, "outputs/reconstructed-client/android/lib/arm64-v8a/libcocos2dlua.so")
    data = bytearray(open(src, "rb").read())
    orig_sha = hashlib.sha256(data).hexdigest()
    for old, new in REPLACEMENTS:
        pat = b"\x00" + old + b"\x00"
        n = bytes(data).count(pat)
        assert n == 1, (old, n)
        assert len(new) <= len(old)
        i = bytes(data).index(pat) + 1
        data[i:i + len(old)] = new + b"\x00" * (len(old) - len(new))
        print("patched %-24s -> %s" % (old.decode(), new.decode()))
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    open(dst, "wb").write(data)
    print("source sha256", orig_sha)
    print("patched sha256", hashlib.sha256(data).hexdigest(), "->", dst)


main()
