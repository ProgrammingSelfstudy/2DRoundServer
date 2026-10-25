#!/usr/bin/env python3
"""Extract the practise tower ("兔丼监狱", holiday_practise_tower_data.lua): constants, the 40 floors and the rank rewards.

usage: extract_practise_tower.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayPractiseTowerData."
m = dump(lua_dir, "holiday_practise_tower_data.lua", [S + n for n in ("data_const", "data_tower", "data_rank_reward")])
m = {k[len(S):]: v for k, v in m.items()}
for k in ("data_tower", "data_rank_reward"):  # tables keyed by number come as objects
    if isinstance(m[k], dict):
        m[k] = [m[k][i] for i in sorted(m[k], key=int)]
const = {k: v["val"] for k, v in m["data_const"].items() if k not in ("rules", "item_show")}
out = {
    "source": "restored client holiday_practise_tower_data.lua",
    "const": const,
    "floors": [{"id": r["id"], "power": r["power"], "reward": r["reward"]} for r in m["data_tower"]],
    "ranks": [{"min": r["min"], "max": r["max"], "items": r["items"]} for r in m["data_rank_reward"]],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["floors"]), "floors;", len(out["ranks"]), "rank rewards")
