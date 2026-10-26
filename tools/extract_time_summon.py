#!/usr/bin/env python3
"""Extract the time summon ("限定召唤", recruit_holiday_data.lua): per camp (activity) the group and the cumulative awards; per
group the costs and the draw odds (the star rows, the up heroes, or for the first groups the heroes with their own weights).

usage: extract_time_summon.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.RecruitHolidayData."
m = dump(lua_dir, "recruit_holiday_data.lua", [S + n for n in ("data_const", "data_action", "data_summon", "data_award", "data_probability")])
m = {k[len(S):]: v for k, v in m.items()}

camps = {}
for camp, a in m["data_action"].items():
    awards = sorted(m["data_award"][camp], key=lambda r: r["id"])
    camps[camp] = {"group": a["group_id"], "awards": [{"id": r["id"], "times": r["times"], "reward": r["reward"]} for r in awards]}
groups = {}
for g, s in m["data_summon"].items():
    rows = [{"id": r["id"], "star": int(r["star"][0]) if r["star"][0].isdigit() else 0, "weight": float(r["probability"])} for r in m["data_probability"][g]]
    for r, src in zip(rows, m["data_probability"][g]):
        r["up"] = src["is_up"] == 1
    groups[g] = {"once": s["loss_item_once"], "ten": s["loss_item_ten"], "gold_once": s["loss_gold_once"], "gold_ten": s["loss_gold_ten"],
                 "gain_once": s["gain_once"], "gain_ten": s["gain_ten"], "rows": rows}
const = {k: v["val"] for k, v in m["data_const"].items()}
json.dump({"source": "restored client recruit_holiday_data.lua", "const": const, "camps": camps, "groups": groups}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("time summon:", len(camps), "camps", len(groups), "groups", const)
