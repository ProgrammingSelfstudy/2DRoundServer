#!/usr/bin/env bash
# A real local admin + login HTTP + TCP gateway, sharing an isolated marked database.
set -euo pipefail
umask 077
cd "$(dirname "$0")/.."
repo="$PWD"
mkdir -p data
root="$(mktemp -d "$repo/data/admin-live.XXXXXX")"
printf '2dround-admin-sandbox\n' > "$root/.admin-ui-sandbox"
printf '2dround-admin-live\n' > "$root/.admin-live-sandbox"
python3 - "$root" <<'PY'
import pathlib,json,secrets,socket,sys
root=pathlib.Path(sys.argv[1])
for port in (19080,19081,19800,19801):
    with socket.socket() as s:s.bind(('127.0.0.1',port))
cfg={'db_path':str(root/'admin.db'),'log_dir':str(root/'logs'),
     'admin':{'addr':'127.0.0.1:19080','jwt_secret':secrets.token_hex(32),'gateway_url':'http://127.0.0.1:19801','backup_dir':str(root/'backups'),'backup_every_hours':0},
     'login_http':{'addr':'127.0.0.1:19081','public_base':'http://127.0.0.1:19081','auto_register':True},
     'gateway':{'tcp_addr':'127.0.0.1:19800','srv_id':'uitest_1','admin_addr':'127.0.0.1:19801','idle_seconds':900,'empty_replies':True},
     'codec':{'byte_order':'big','str_len_bytes':2,'arr_len_bytes':2,'str_null_term':False,'confirmed':True}}
(root/'config.yaml').write_text(json.dumps(cfg))
PY
go run ./tools/adminfixture -dir "$root"
# Run from the isolated data directory; runtime needs no dist/source checkout.
cd "$root"
"$repo/bin/loginhttp" -config "$root/config.yaml" > "$root/login.stdout.log" 2>&1 &
login_pid=$!
"$repo/bin/gateway" -config "$root/config.yaml" -v=false > "$root/gateway.stdout.log" 2>&1 &
gateway_pid=$!
"$repo/bin/admin" -config "$root/config.yaml" > "$root/admin.stdout.log" 2>&1 &
admin_pid=$!
printf '{"admin":%s,"gateway":%s,"login":%s}\n' "$admin_pid" "$gateway_pid" "$login_pid" > "$root/pids.json"
cleanup() {
  # pids.json also records a deliberate restart made during a local failure test.
  python3 - "$root" <<'PYEND'
import pathlib,json,os,signal,subprocess,sys
root=pathlib.Path(sys.argv[1])
for pid in json.loads((root/'pids.json').read_text()).values():
    cmd=subprocess.run(['ps','-p',str(pid),'-o','command='],capture_output=True,text=True).stdout
    if str(root/'config.yaml') in cmd:
        try:os.kill(pid,signal.SIGTERM)
        except ProcessLookupError:pass
PYEND
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 - <<'PYREADY'
import time,urllib.request
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for endpoint in ('http://127.0.0.1:19080/api/healthz','http://127.0.0.1:19081/api/serverlist','http://127.0.0.1:19801/online'):
    for attempt in range(40):
        try:
            with opener.open(endpoint,timeout=1) as response:
                if response.status==200:break
        except Exception:time.sleep(.2)
    else:raise SystemExit('Isolated service did not become ready: '+endpoint)
PYREADY
printf 'Real isolated Go admin: http://127.0.0.1:19080/\nSandbox/evidence/0600 credentials: %s\n' "$root"
wait
