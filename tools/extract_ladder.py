#!/usr/bin/env python3
"""Extract the sky ladder (天梯): sky_ladder_data.lua, the constants, the rank rewards and the price of the extra challenges.

usage: extract_ladder.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.SkyLadderData."
names = ("data_const", "data_award", "data_buy_num")
m = dump(lua_dir, "sky_ladder_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


out = {
    "source": "restored client sky_ladder_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items() if k not in ("role_box", "close_text")},
    "award": [{"min": r["min"], "max": r["max"], "reward": r["reward"]} for r in rows(m["data_award"])],
    "buy": [{"cost": r["cost"], "vip": r["vip"]} for r in rows(m["data_buy_num"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
