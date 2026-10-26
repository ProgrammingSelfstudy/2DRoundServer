#!/usr/bin/env python3
"""Extract the new war order ("海贼夺宝", the pirate treasure, holiday_new_war_order_data.lua): per period the level rows and
the day / week tasks, each in the bands of the role level (min_lev - max_lev).

usage: extract_newwarorder.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayNewWarOrderData."
m = dump(lua_dir, "holiday_new_war_order_data.lua", [S + n for n in ("data_constant", "data_lev_reward_list", "data_day_task_list", "data_week_task_list")])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    if isinstance(t, dict):
        return {int(k): v for k, v in t.items()}
    return {i + 1: v for i, v in enumerate(t)}


def vals(x):
    return list(x.values()) if isinstance(x, dict) else x


def task(g, kind):
    p = g["progress"][0]
    t = {"id": g["goal_id"], "kind": kind, "desc": g["desc"], "label": p["cli_label"], "need": p["target_val"], "param": p["param"],
         "award": g["award"], "min_lev": g["min_lev"], "max_lev": g["max_lev"]}
    t["min"], t["max"] = (g["day"], g["day"]) if kind == 1 else (g["min_day"], g["max_day"])
    return t


periods = {}
for p, levs in rows(m["data_lev_reward_list"]).items():
    periods[p] = {"levs": [{"lev": l["lev"], "exp": l["exp"], "min_lev": l["min_lev"], "max_lev": l["max_lev"], "reward": l["reward"],
                            "rmb_reward": l["rmb_reward"]} for l in sorted(vals(levs), key=lambda l: (l["min_lev"], l["lev"]))], "tasks": []}
for name, kind in (("data_day_task_list", 1), ("data_week_task_list", 2)):
    for p, ts in rows(m[name]).items():
        periods[p]["tasks"] += [task(g, kind) for g in sorted(vals(ts), key=lambda g: g["goal_id"])]
json.dump({"source": "restored client holiday_new_war_order_data.lua", "levmax": m["data_constant"]["war_order_levmax"]["val"],
           "periods": {str(k): v for k, v in periods.items()}}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("newwarorder:", {p: (len(v["levs"]), len(v["tasks"])) for p, v in periods.items()})
