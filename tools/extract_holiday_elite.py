#!/usr/bin/env python3
"""Extract the holiday elite summon ("新春赐福" and its kin, recruit_holiday_elite_data.lua): per camp (activity) the group, the
cumulative awards; per group the costs, the prize pool (with its weights) and the up prizes.

usage: extract_holiday_elite.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.RecruitHolidayEliteData."
m = dump(lua_dir, "recruit_holiday_elite_data.lua", [S + n for n in ("data_const", "data_action", "data_summon", "data_award", "data_probability", "data_hero_show")])
m = {k[len(S):]: v for k, v in m.items()}


def vals(x):
    return list(x.values()) if isinstance(x, dict) else x


camps = {}
for camp, a in m["data_action"].items():
    awards = sorted(vals(m["data_award"][camp]), key=lambda r: r["id"])
    camps[camp] = {"group": a["group_id"], "awards": [{"id": r["id"], "times": r["times"], "reward": r["reward"]} for r in awards]}
groups = {}
for g, s in m["data_summon"].items():
    pool = [{"id": r["id"], "num": r["num"], "weight": float(r["probability"])} for r in vals(m["data_hero_show"][g])]
    ups = [{"id": r["id"], "num": r["num"]} for r in vals(m["data_probability"][g]) if r["is_up"] == 1 and r["id"] > 0]
    groups[g] = {"once": s["loss_item_once"], "ten": s["loss_item_ten"], "gold_once": s["loss_gold_once"], "gold_ten": s["loss_gold_ten"],
                 "gain_once": s["gain_once"], "gain_ten": s["gain_ten"], "pool": pool, "ups": ups}
const = {k: v["val"] for k, v in m["data_const"].items()}
json.dump({"source": "restored client recruit_holiday_elite_data.lua", "const": const, "camps": camps, "groups": groups}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("holiday elite:", len(camps), "camps", len(groups), "groups", const)
