#!/usr/bin/env python3
"""Extract the guild war (公会战, Config.GuildWarData) from the restored client: the constants (times, attempts, difficulty factors, the
rewards of a fight and of the guilds), the war score of the 50 positions by difficulty, the guild buff levels, the power of the
neutral guards by level and the rewards of the war score rank.

usage: extract_guildwar.py LUA_DIR OUT_JSON
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.GuildWarData."
m = dump(lua_dir, "guild_war_data.lua", [K + n for n in ("data_const", "data_award", "data_position", "data_buff", "data_guard_power")])
const = {k: v["val"] for k, v in m[K + "data_const"].items() if not isinstance(v["val"], str)}
out = {"source": "restored client guild_war_data.lua", "const": const,
       "positions": [[w[1] for w in p["warscore"]] for p in m[K + "data_position"]],
       "buff": [{att[0]: att[1] for att in b["attr"]} for b in m[K + "data_buff"]],
       "guard_power": [g["power"] for g in m[K + "data_guard_power"]],
       "rank_awards": [{"num": a["num"], "items": a["award"]} for a in sorted(m[K + "data_award"].values(), key=lambda a: a["num"])]}
assert len(out["positions"]) == 50 and len(out["buff"]) == 15
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["positions"]), "positions;", len(out["buff"]), "buff levels;", len(out["rank_awards"]), "rank awards")
