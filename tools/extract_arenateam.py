#!/usr/bin/env python3
"""Extract the team arena (组队竞技场): arena_team_data.lua, the constants, the level and power limits, the challenge-count boxes, the rank rewards.

usage: extract_arenateam.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.ArenaTeamData."
names = ("data_const", "data_level_info", "data_power_info", "data_challenge_count_reward_info", "data_rank_reward")
m = dump(lua_dir, "arena_team_data.lua", [S + n for n in names])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, list):
        return t
    return [t[k] for k in sorted(t, key=int)]


out = {
    "source": "restored client arena_team_data.lua",
    "const": {k: v["val"] for k, v in m["data_const"].items() if k != "robot_headicon"},
    "faces": m["data_const"]["robot_headicon"]["val"],
    "levels": [r["lev"] for r in rows(m["data_level_info"])],
    "powers": [r["power"] for r in rows(m["data_power_info"])],
    "boxes": [{"id": r["id"], "count": r["count"], "award": r["award"]} for r in rows(m["data_challenge_count_reward_info"])],
    "ranks": [{"min": r["min"], "max": r["max"], "items": r["items"]} for r in sorted(m["data_rank_reward"].values(), key=lambda r: r["min"])],
}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print({k: len(v) for k, v in out.items() if k != "source"})
