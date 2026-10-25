#!/usr/bin/env python3
"""Extract the hero expedition ("英雄远征") from the restored client (expedition_data.lua): constants, the 20 map points
(15 floors and a box after every third) and the three difficulties with their power gate and total rewards.

usage: extract_expedition.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ExpeditionData."
m = dump(lua_dir, "expedition_data.lua", [S + n for n in ("data_const", "data_sign_info", "data_sign_reward")])
m = {k[len(S):]: v for k, v in m.items()}
const = {k: v["val"] for k, v in m["data_const"].items() if k != "game_rule"}
out = {
    "source": "restored client expedition_data.lua",
    "const": const,
    "points": [{"id": p["id"], "floor": p["floor"], "type": p["type"]} for p in m["data_sign_info"]],
    "modes": [{"id": r["id"], "power": r["power"], "items": r["items"]} for r in m["data_sign_reward"]],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["points"]), "points;", len(out["modes"]), "modes")
