#!/usr/bin/env python3
"""Extract the privileges of the top-up window (privilege_data.lua): what each one is (the product it is bought with or the diamonds it
costs, how long it lasts, the level it needs) and what it pays by the role's level.

usage: extract_privilege.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.PrivilegeData."
m = dump(lua_dir, "privilege_data.lua", [K + "data_privilege_data", K + "data_privilege_award"])
rows = m[K + "data_privilege_data"]
rows = list(rows.values()) if isinstance(rows, dict) else rows
out = {
    "source": "restored client privilege_data.lua",
    "privileges": [{"id": r["id"], "name": r["name"], "charge_id": r["charge_id"], "pay_type": r["pay_type"], "loss": r["loss"],
                    "limit_type": r["limit_type"], "limit_day": r["limit_day"], "limit_lev": r["limit_lev"], "sort": r["sort_id"]}
                   for r in sorted(rows, key=lambda r: r["id"])],
    "award": {str(k): [{"min": r["min"], "max": r["max"], "items": r["reward"]} for r in v]
              for k, v in sorted(m[K + "data_privilege_award"].items(), key=lambda kv: int(kv[0]))},
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print("privileges:", len(out["privileges"]), "award groups:", len(out["award"]))
