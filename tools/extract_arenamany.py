#!/usr/bin/env python3
"""Extract the holiday multi-player arena ("多人竞技场", holiday_arena_team_data.lua): constants, the 12 score levels,
the level box rewards and the rank rewards.

usage: extract_arenamany.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayArenaTeamData."
m = dump(lua_dir, "holiday_arena_team_data.lua", [S + n for n in ("data_const", "data_elite_level", "data_elite_award", "data_rank_reward", "data_battle_pay")])
m = {k[len(S):]: v for k, v in m.items()}


def seq(v):
    return [v[i] for i in sorted(v, key=int)] if isinstance(v, dict) else v


const = {k: v["val"] for k, v in m["data_const"].items()}
levels = [{"lev": l["lev"], "name": l["name"], "need": l["need_score"], "base": l["base_add_score"]} for l in seq(m["data_elite_level"])]
awards = [{"lev": a["lev"], "award": a["award"]} for a in seq(m["data_elite_award"])]
ranks = [{"min": r["min"], "max": r["max"], "items": r["items"]} for r in seq(m["data_rank_reward"])]
pay = [{"count": p["count"], "expend": p["expend"][0]} for p in seq(m["data_battle_pay"])]
assert len(levels) == 12 and awards and ranks and pay
json.dump({"source": "restored client holiday_arena_team_data.lua", "const": const, "levels": levels, "awards": awards, "ranks": ranks, "pay": pay},
          open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("arenamany:", len(levels), "levels", len(awards), "awards", len(ranks), "ranks", len(pay), "pay")
