#!/usr/bin/env bash
# Disposable admin API for frontend development. Never opens configs/dev.yaml.
set -euo pipefail
cd "$(dirname "$0")/.."
port="${ADMIN_SANDBOX_PORT:-18080}"
python3 - "$port" <<'PY'
import socket,sys
port=int(sys.argv[1])
with socket.socket() as s:
    s.bind(('127.0.0.1', port))
PY
sandbox_dir="$(mktemp -d "${TMPDIR:-/tmp}/2dround-admin.XXXXXX")"
printf '2dround-admin-sandbox\n' > "$sandbox_dir/.admin-ui-sandbox"
admin_pid=""
cleanup() {
  if [[ -n "$admin_pid" ]]; then
    kill "$admin_pid" 2>/dev/null || true
    wait "$admin_pid" 2>/dev/null || true
  fi
  rm -rf "$sandbox_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 - "$sandbox_dir" "$port" <<'PY'
import json,pathlib,secrets,sys
root=pathlib.Path(sys.argv[1])
# JSON is a YAML subset. Keep all data and credentials under the temporary directory.
config={
  'db_path':str(root/'admin.db'),
  'admin':{'addr':'127.0.0.1:'+sys.argv[2], 'jwt_secret':secrets.token_hex(32),
           'backup_dir':str(root/'backups'), 'backup_every_hours':0},
  'gateway':{'admin_addr':''}
}
(root/'config.yaml').write_text(json.dumps(config))
PY
if [[ "${ADMIN_SANDBOX_FIXTURES:-0}" == "1" ]]; then
  go run ./tools/adminfixture -dir "$sandbox_dir"
fi
printf 'Building isolated admin API; temporary database: %s/admin.db\n' "$sandbox_dir"
go build -o "$sandbox_dir/admin" ./cmd/admin
printf 'Disposable API and embedded Vue UI: http://127.0.0.1:%s/ (Ctrl-C stops and deletes sandbox)\n' "$port"
"$sandbox_dir/admin" -config "$sandbox_dir/config.yaml" &
admin_pid=$!
wait "$admin_pid"
