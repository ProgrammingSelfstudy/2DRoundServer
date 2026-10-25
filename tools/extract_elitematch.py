#!/usr/bin/env python3
"""Extract the elite match (精英段位赛): arena_elite_data.lua, the constants, the 22 ranks, the price of the extra challenges.

usage: extract_elitematch.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ArenaEliteData."
names = ("data_elite_const", "data_elite_level", "data_elite_buy")
m = dump(lua_dir, "arena_elite_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


out = {
    "source": "restored client arena_elite_data.lua",
    "const": {k: v["val"] for k, v in m["data_elite_const"].items()},
    "level": [{k: r[k] for k in ("id", "name", "next_id", "need_exp", "init_exp", "need_promoted", "award", "lev_award", "promoted_info")}
              for r in rows(m["data_elite_level"])],
    "buy": [{"cost": r["cost"], "vip": r["need_vip"]} for r in rows(m["data_elite_buy"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
