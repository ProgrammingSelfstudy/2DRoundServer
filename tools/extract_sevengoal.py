#!/usr/bin/env python3
"""Extract the seven-day goals ("七日目标": the adventure diary and the magic box, day_goals_new_data.lua): per period the
goals (day goals and the accumulated-charge goals), the level rows and the item that counts as the experience.

usage: extract_sevengoal.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.DayGoalsNewData."
m = dump(lua_dir, "day_goals_new_data.lua", [S + n for n in ("data_group_list", "data_charge_list", "data_make_lev_list", "data_circle_exp_item")])
m = {k[len(S):]: v for k, v in m.items()}

periods = {}
for p, goals in m["data_group_list"].items():
    rows = []
    for gid in sorted(goals, key=int):
        g = goals[gid]
        rows.append({"id": int(gid), "desc": g["desc"], "award": g["award"]})
    periods[p] = {"goals": rows}
for p, goals in m["data_charge_list"].items():
    periods[p]["charge"] = [{"id": g["goal_id"], "desc": g["desc"], "award": g["award"], "rmb": g["progress"][0]["target_val"]}
                            for g in sorted(goals.values(), key=lambda g: g["id"])]
for p, levs in m["data_make_lev_list"].items():
    periods[p]["levs"] = [{"lev": l["lev"], "exp": l["exp"], "reward": l["reward"]} for l in sorted(levs, key=lambda l: l["lev"])]
for p, it in m["data_circle_exp_item"].items():
    periods[p]["exp_item"] = it["item_id"]
assert all({"goals", "charge", "levs", "exp_item"} <= set(v) for v in periods.values()), periods.keys()
json.dump({"source": "restored client day_goals_new_data.lua", "periods": periods}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("sevengoal:", {p: (len(v["goals"]), len(v["charge"]), len(v["levs"])) for p, v in periods.items()})
