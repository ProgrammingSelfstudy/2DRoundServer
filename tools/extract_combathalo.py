#!/usr/bin/env python3
"""Extract the camp halos of a battle (combat_halo_data.lua): which mix of camps (camp, head count) gives which halo, which weaker
halo it replaces (`condition`) and the attributes it adds.

usage: extract_combathalo.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.CombatHaloData."
m = dump(lua_dir, "combat_halo_data.lua", [S + "data_halo"])
t = m[S + "data_halo"]
rows = t if isinstance(t, list) else [t[k] for k in sorted(t, key=int)]
out = {
    "source": "restored client combat_halo_data.lua",
    "halos": [{"id": r["id"], "condition": r["condition"], "pos_info": r["pos_info"], "attrs": r.get("attrs", [])} for r in rows],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({"halos": len(out["halos"])})
