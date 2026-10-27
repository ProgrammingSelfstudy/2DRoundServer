#!/usr/bin/env python3
"""Extract the primus play (霸气修炼 / 霸主之位, Config.PrimusData): the six seats (arena rank asked, title, look) and the look and unit
of the boss of a seat by how many times it evolved.

usage: extract_primus.py LUA_DIR OUT_JSON
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.PrimusData."
m = dump(lua_dir, "primus_data.lua", [K + n for n in ("data_const", "data_upgrade", "data_unitdata")])
out = {"source": "restored client primus_data.lua",
       "const": {k: v["val"] for k, v in m[K + "data_const"].items() if isinstance(v["val"], int)},
       "seats": [{"pos": s["pos"], "rank": s["arena_rank"], "name": s["name"], "honor": s["honor_id"], "look": s["look_id"]} for s in sorted(m[K + "data_upgrade"], key=lambda s: s["pos"])],
       "units": sorted(({"pos": u["pos"], "min": u["min"], "max": u["max"], "unit": u["unit_id"], "look": u["look_id"]} for u in m[K + "data_unitdata"].values()), key=lambda u: (u["pos"], u["min"]))}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["seats"]), "seats;", len(out["units"]), "units")
