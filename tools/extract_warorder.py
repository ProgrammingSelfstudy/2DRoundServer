#!/usr/bin/env python3
"""Extract the war order ("战令", the holiday war order, holiday_war_order_data.lua): per period the level rows
(cumulative experience, free and paid rewards), the day / week / period tasks and the constants.

usage: extract_warorder.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.HolidayWarOrderData."
m = dump(lua_dir, "holiday_war_order_data.lua", [S + n for n in ("data_constant", "data_lev_reward_list", "data_day_task_list", "data_week_task_list", "data_period_task_list", "data_advance_card_list")])
m = {k[len(S):]: v for k, v in m.items()}


def rows(t):
    """the per period tables are lists indexed from 1 (a period) of objects or lists"""
    if isinstance(t, dict):
        return {int(k): v for k, v in t.items()}
    return {i + 1: v for i, v in enumerate(t)}


def vals(x):
    return list(x.values()) if isinstance(x, dict) else x


def task(g, kind):
    p = g["progress"][0]
    t = {"id": g["goal_id"], "kind": kind, "desc": g["desc"], "label": p["cli_label"], "need": p["target_val"],
         "param": p["param"], "award": g["award"]}
    if kind == 1:
        t["min"] = t["max"] = g["day"]
    elif kind == 2:
        t["min"], t["max"] = g["min_day"], g["max_day"]
    return t


periods = {}
for p, levs in rows(m["data_lev_reward_list"]).items():
    periods[p] = {"levs": [{"lev": l["lev"], "exp": l["exp"], "reward": l["reward"], "rmb_reward": l["rmb_reward"]}
                           for l in sorted(vals(levs), key=lambda l: l["lev"])], "tasks": []}
for name, kind in (("data_day_task_list", 1), ("data_week_task_list", 2), ("data_period_task_list", 3)):
    for p, ts in rows(m[name]).items():
        periods[p]["tasks"] += [task(g, kind) for g in sorted(vals(ts), key=lambda g: g["goal_id"])]
for p, cards in rows(m["data_advance_card_list"]).items():
    periods[p]["card"] = [{"id": c["id"], "charge_id": c["charge_id"], "reward": c["reward"]} for c in vals(cards)]
levmax = m["data_constant"]["war_order_levmax"]["val"]
json.dump({"source": "restored client holiday_war_order_data.lua", "levmax": levmax, "periods": {str(k): v for k, v in periods.items()}},
          open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("warorder:", {p: (len(v["levs"]), len(v["tasks"])) for p, v in periods.items()}, "levmax", levmax)
