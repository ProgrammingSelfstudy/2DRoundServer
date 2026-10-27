#!/usr/bin/env python3
"""Build a client APK that talks to a server on the local network.

The patched client of tools/patch_client_urls.py only works with loopback addresses
(the URLs inside the Lua bytecode are edited in place and cannot grow). This script
instead rewrites the host at run time: every http URL the client opens through
Cocos2dxHttpURLConnection.createHttpURLConnection whose host is one of the original
servers, the reachability probe hosts, or a loopback spelling, is sent to BASE
instead (scheme, host and port replaced, path and query kept). The game TCP
connection goes where the server list says, but the native library only connects to
hosts on its built-in white list, so the server's address is put on that list too.

It does this with a generated overlay for reconstructed-client/build.py:
  smali/org/local/LanRewrite.smali                         the rewriting function
  smali/org/cocos2dx/lib/Cocos2dxHttpURLConnection.smali  a copy that calls it first
  smali/org/local/PayOpen.smali + smali/com/game/core/GameActivity.smali  the pay call opens the payment page in the browser
  lib/arm64-v8a/libcocos2dlua.so  the native library with BASE's host in its connection white list
and a copy of the Lua overrides whose config.lua names BASE for the url_config download,
which the native downloader does (not Java).

usage: make_lan_client.py CLIENT_ROOT BASE [--output PATH]
  CLIENT_ROOT  the restored client tree (contains outputs/reconstructed-client)
  BASE         e.g. http://192.168.110.186:8081 (the login HTTP service)
"""
import argparse, os, re, shutil, subprocess, sys, tempfile
from pathlib import Path

HOSTS = ["211.154.26.201", "14.29.249.106", "117.24.14.112", "121.204.252.126",
         "www.baidu.com", "ff.y17.xyz", "127.0.0.1", "127.1", "localhost"]

HEAD = """.class public Lorg/local/LanRewrite;
.super Ljava/lang/Object;

.field private static final BASE:Ljava/lang/String; = "%s"

# Returns url with "http://host[:port]" replaced by BASE when host is a known
# original / loopback host; anything else (https, other hosts, null) is returned as is.
.method public static rewrite(Ljava/lang/String;)Ljava/lang/String;
    .locals 6

    if-nez p0, :nonnull

    return-object p0

    :nonnull
    const-string v0, "http://"

    invoke-virtual {p0, v0}, Ljava/lang/String;->startsWith(Ljava/lang/String;)Z

    move-result v1

    if-nez v1, :is_http

    return-object p0

    :is_http
    const/4 v1, 0x7

    invoke-virtual {p0}, Ljava/lang/String;->length()I

    move-result v2

    move v3, v1

    :host_loop
    if-ge v3, v2, :host_done

    invoke-virtual {p0, v3}, Ljava/lang/String;->charAt(I)C

    move-result v4

    const/16 v5, 0x2f

    if-eq v4, v5, :host_done

    const/16 v5, 0x3f

    if-eq v4, v5, :host_done

    const/16 v5, 0x3a

    if-eq v4, v5, :host_done

    add-int/lit8 v3, v3, 0x1

    goto :host_loop

    :host_done
    invoke-virtual {p0, v1, v3}, Ljava/lang/String;->substring(II)Ljava/lang/String;

    move-result-object v4
"""

CHECK = """
    const-string v5, "%s"

    invoke-virtual {v4, v5}, Ljava/lang/String;->equals(Ljava/lang/Object;)Z

    move-result v5

    if-nez v5, :matched
"""

TAIL = """
    return-object p0

    :matched
    if-ge v3, v2, :port_done

    invoke-virtual {p0, v3}, Ljava/lang/String;->charAt(I)C

    move-result v4

    const/16 v5, 0x3a

    if-ne v4, v5, :port_done

    add-int/lit8 v3, v3, 0x1

    :port_loop
    if-ge v3, v2, :port_done

    invoke-virtual {p0, v3}, Ljava/lang/String;->charAt(I)C

    move-result v4

    const/16 v5, 0x30

    if-lt v4, v5, :port_done

    const/16 v5, 0x39

    if-gt v4, v5, :port_done

    add-int/lit8 v3, v3, 0x1

    goto :port_loop

    :port_done
    invoke-virtual {p0, v3}, Ljava/lang/String;->substring(I)Ljava/lang/String;

    move-result-object v4

    sget-object v5, Lorg/local/LanRewrite;->BASE:Ljava/lang/String;

    invoke-virtual {v5, v4}, Ljava/lang/String;->concat(Ljava/lang/String;)Ljava/lang/String;

    move-result-object v4

    return-object v4
.end method
"""


def lan_rewrite_smali(base):
    return HEAD % base + "".join(CHECK % h for h in HOSTS) + TAIL


def patch_connection(src):
    """Insert the call at the start of createHttpURLConnection(String)."""
    marker = ".method static createHttpURLConnection(Ljava/lang/String;)Ljava/net/HttpURLConnection;"
    i = src.index(marker)
    j = src.index(".prologue", i)
    j = src.index("\n", j) + 1
    call = ("\n    invoke-static {p0}, Lorg/local/LanRewrite;->rewrite(Ljava/lang/String;)Ljava/lang/String;\n\n"
            "    move-result-object p0\n")
    assert src.count(marker) == 1 and "LanRewrite" not in src
    return src[:j] + call + src[j:]


# The client's pay call (sdkOnPay -> GameActivity.pay(json)) is an empty stub in the rebuilt client. The overlay makes it open the
# phone's browser on BASE/pay/start?info=<json>, the server's payment page: it opens an order for the role and shows the QR
# codes the operator set in the admin page (internal/login/pay.go).
PAY_OPEN = """.class public Lorg/local/PayOpen;
.super Ljava/lang/Object;

.field private static final BASE:Ljava/lang/String; = "%s"

.method public static open(Landroid/app/Activity;Ljava/lang/String;)V
    .locals 5

    if-eqz p0, :done

    if-eqz p1, :done

    :try_start
    const-string v0, "UTF-8"

    invoke-static {p1, v0}, Ljava/net/URLEncoder;->encode(Ljava/lang/String;Ljava/lang/String;)Ljava/lang/String;

    move-result-object v0

    new-instance v1, Ljava/lang/StringBuilder;

    invoke-direct {v1}, Ljava/lang/StringBuilder;-><init>()V

    sget-object v2, Lorg/local/PayOpen;->BASE:Ljava/lang/String;

    invoke-virtual {v1, v2}, Ljava/lang/StringBuilder;->append(Ljava/lang/String;)Ljava/lang/StringBuilder;

    const-string v2, "/pay/start?info="

    invoke-virtual {v1, v2}, Ljava/lang/StringBuilder;->append(Ljava/lang/String;)Ljava/lang/StringBuilder;

    invoke-virtual {v1, v0}, Ljava/lang/StringBuilder;->append(Ljava/lang/String;)Ljava/lang/StringBuilder;

    invoke-virtual {v1}, Ljava/lang/StringBuilder;->toString()Ljava/lang/String;

    move-result-object v1

    invoke-static {v1}, Landroid/net/Uri;->parse(Ljava/lang/String;)Landroid/net/Uri;

    move-result-object v1

    new-instance v2, Landroid/content/Intent;

    const-string v3, "android.intent.action.VIEW"

    invoke-direct {v2, v3, v1}, Landroid/content/Intent;-><init>(Ljava/lang/String;Landroid/net/Uri;)V

    const/high16 v3, 0x10000000

    invoke-virtual {v2, v3}, Landroid/content/Intent;->addFlags(I)Landroid/content/Intent;

    invoke-virtual {p0, v2}, Landroid/app/Activity;->startActivity(Landroid/content/Intent;)V
    :try_end
    .catch Ljava/lang/Exception; {:try_start .. :try_end} :done

    :done
    return-void
.end method
"""


def patch_pay(src):
    """Make GameActivity.pay(String) call PayOpen.open(this, info)."""
    marker = ".method public pay(Ljava/lang/String;)V"
    i = src.index(marker)
    j = src.index(".end method", i)
    body = (marker + "\n    .locals 0\n    .param p1, \"info\"    # Ljava/lang/String;\n\n    .prologue\n"
            "    invoke-static {p0, p1}, Lorg/local/PayOpen;->open(Landroid/app/Activity;Ljava/lang/String;)V\n\n    return-void\n")
    assert src.count(marker) == 1 and "PayOpen" not in src
    return src[:i] + body + src[j:]


CONFIG_HASH = "99F709C18721A583"  # config.luac


def lan_native_library(client_root, base, overlay):
    """The native library with its host strings patched for BASE.

    SmartSocket::ConnectAsyn refuses (returns -1, silently) every host that is not
    contained in Data::getWhiteList(), which is the three host strings of the library's
    AppDelegate. tools/patch_native_strings.py turns them into loopback; here one of them
    becomes the server's own address, so the game connection may go to the LAN. The
    patched strings keep their length (NUL padded), so no offset changes."""
    host = re.fullmatch(r"http://([0-9.]+):\d+", base).group(1)
    assert len(host) <= len("103.217.200.127"), "the address must fit the 15 characters of 103.217.200.127"
    src = Path(client_root) / "outputs" / "apk-extracted" / "lib" / "arm64-v8a" / "libcocos2dlua.so"
    data = bytearray(src.read_bytes())
    for old, new in [(b"http://www.baidu.com", b"http://127.1:8081"),   # reachability probes: Java rewrites them
                     (b"http://ff.y17.xyz", b"http://127.1:8081"),
                     (b"103.217.200.127", host.encode()),             # white list: the server's address
                     (b"211.154.26.201", b"127.0.0.1")]:              # white list: loopback (adb reverse)
        pat = b"\x00" + old + b"\x00"
        assert bytes(data).count(pat) == 1, old
        assert len(new) <= len(old)
        i = bytes(data).index(pat) + 1
        data[i:i + len(old)] = new + b"\x00" * (len(old) - len(new))
    dst = overlay / "lib" / "arm64-v8a" / "libcocos2dlua.so"
    dst.parent.mkdir(parents=True)
    dst.write_bytes(bytes(data))


def lan_lua_overrides(root, client_root, base, work):
    """A copy of the client's Lua overrides whose config.lua points the native downloader
    (which fetches url_config and does not go through Java) at BASE. Everything else in
    config.lua stays as the decompiler wrote it, apart from skipping the hot update."""
    out = work / "lua-overrides"
    shutil.copytree(root / "lua-overrides", out)
    src = (Path(client_root) / "outputs" / "readable-source" / "lua" / "config.lua").read_text(encoding="utf-8")
    for pat, new in [(r"^UPDATE_SKIP = .*$", "UPDATE_SKIP = true"),
                     (r"^URL_LUA_CONFIG = .*$", 'URL_LUA_CONFIG = "%s/update/config/url_config_demo.lua"' % base),
                     (r"^LOG_URL = .*$", 'LOG_URL = "%s/log"' % base)]:
        src, n = re.subn(pat, lambda _m, new=new: new, src, flags=re.M)
        assert n == 1, pat
    cfg = work / "config.lua"
    cfg.write_text(src, encoding="utf-8")
    cmd = [sys.executable, str(root / "patch_lua.py"), "--hash", CONFIG_HASH, "--source", str(cfg), "--output-dir", str(out)]
    lj = shutil.which("luajit")
    if lj:
        cmd += ["--luajit", lj]
    env = dict(os.environ)
    # a Homebrew LuaJIT needs its jit.* modules on LUA_PATH to compile bytecode (-b)
    for d in sorted(Path("/usr/local/Cellar/luajit").glob("*/share/luajit-*")) + sorted(Path("/opt/homebrew/Cellar/luajit").glob("*/share/luajit-*")):
        env["LUA_PATH"] = "%s/?.lua;;" % d
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, env=env)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("client_root")
    ap.add_argument("base")
    ap.add_argument("--output")
    ap.add_argument("--no-build", action="store_true", help="only write the overlay and print its path")
    a = ap.parse_args()
    if not re.fullmatch(r"http://[0-9.]+:\d+", a.base):
        sys.exit("BASE must look like http://192.168.1.10:8081")
    root = Path(a.client_root) / "outputs" / "reconstructed-client"
    smali = root / "android" / "smali" / "org" / "cocos2dx" / "lib" / "Cocos2dxHttpURLConnection.smali"
    out = Path(a.output) if a.output else root / "dist" / "local-lan.apk"

    overlay = Path(tempfile.mkdtemp(prefix="lan-overlay-"))
    (overlay / "smali" / "org" / "local").mkdir(parents=True)
    (overlay / "smali" / "org" / "cocos2dx" / "lib").mkdir(parents=True)
    (overlay / "smali" / "org" / "local" / "LanRewrite.smali").write_text(lan_rewrite_smali(a.base), encoding="utf-8")
    (overlay / "smali" / "org" / "cocos2dx" / "lib" / "Cocos2dxHttpURLConnection.smali").write_text(
        patch_connection(smali.read_text(encoding="utf-8")), encoding="utf-8")
    (overlay / "smali" / "org" / "local" / "PayOpen.smali").write_text(PAY_OPEN % a.base, encoding="utf-8")
    game = root / "android" / "smali" / "com" / "game" / "core" / "GameActivity.smali"
    (overlay / "smali" / "com" / "game" / "core").mkdir(parents=True)
    (overlay / "smali" / "com" / "game" / "core" / "GameActivity.smali").write_text(patch_pay(game.read_text(encoding="utf-8")), encoding="utf-8")
    lan_native_library(a.client_root, a.base, overlay)
    print("overlay:", overlay)
    overrides = lan_lua_overrides(root, a.client_root, a.base, Path(tempfile.mkdtemp(prefix="lan-lua-")))
    print("lua overrides:", overrides)
    if a.no_build:
        return
    cmd = [sys.executable, str(root / "build.py"), "--package-name", "org.local.onepiece.reconstructed",
           "--app-label", "航海冒险重建测试", "--output", str(out), "--report", str(root / "build-report-local-lan.json"),
           "--overlay", str(overlay), "--lua-overrides", str(overrides), "--skip-dex-verify", "--allow-changed", "classes.dex",
           "--allow-changed", "lib/arm64-v8a/libcocos2dlua.so"]
    print("running:", " ".join(cmd))
    sys.exit(subprocess.run(cmd).returncode)


if __name__ == "__main__":
    main()
