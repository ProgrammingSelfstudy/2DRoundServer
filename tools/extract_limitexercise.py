#!/usr/bin/env python3
"""Extract the limit exercise ("试炼之境", holiday_boss_new_data.lua): constants, the bosses of the levels and the settle rewards.

usage: extract_limitexercise.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayBossNewData."
m = dump(lua_dir, "holiday_boss_new_data.lua", [S + n for n in ("data_const", "data_round_list", "data_lev_reward_list", "data_change_boss_list")])
m = {k[len(S):]: v for k, v in m.items()}


def seq(v):  # a table keyed by numbers comes as an object
    return [v[i] for i in sorted(v, key=int)] if isinstance(v, dict) else v


const = {k: v["val"] for k, v in m["data_const"].items() if k not in ("game_rule1",)}
rounds = []  # rounds[r][d][t] = levels of difficulty d+1, area t+1 of round r+1
for r in seq(m["data_change_boss_list"]):
    diffs = []
    for d in seq(r):
        areas = []
        for t in seq(d):
            areas.append([{"order": b["order_id"], "name": b["name"], "power": b["power"], "head": b["head_id"], "desc": b["add_skill_decs"]} for b in seq(t)])
        diffs.append(areas)
    rounds.append(diffs)
rewards = [[{"order": x["order_id"], "reward": x["reward"]} for x in seq(d)] for d in seq(m["data_lev_reward_list"])]
out = {"source": "restored client holiday_boss_new_data.lua", "const": const, "rounds": rounds, "rewards": rewards}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(rounds), "rounds;", [len(r) for r in rounds], "difficulties;", len(rewards), "reward tiers;", [[len(a) for a in d] for d in rounds[0][:2]])
