#!/usr/bin/env python3
"""Extract the egg smashing event (break_egg_data.lua): the constants and the list of best prizes shown.

usage: extract_smashegg.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.BreakEggData."
m = dump(lua_dir, "break_egg_data.lua", [S + n for n in ("data_const", "data_award")])
m = {k[len(S):]: v for k, v in m.items()}
if isinstance(m["data_award"], dict):
    m["data_award"] = [m["data_award"][i] for i in sorted(m["data_award"], key=int)]
out = {
    "source": "restored client break_egg_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items() if k != "holiday_rule"},
    "awards": [{"bid": r["bid"], "num": r["num"]} for r in m["data_award"]],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["const"]), "constants;", len(out["awards"]), "awards")
