#!/usr/bin/env python3
"""Extract the arena champion match (冠军赛): arena_champion_data.lua, the constants and the rank rewards.

usage: extract_arenachampion.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ArenaChampionData."
names = ("data_const", "data_awards")
m = dump(lua_dir, "arena_champion_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


out = {
    "source": "restored client arena_champion_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items()},
    "awards": [{"min": r["min"], "max": r["max"], "items": r["items"]} for r in rows(m["data_awards"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
