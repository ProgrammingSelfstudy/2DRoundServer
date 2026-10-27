#!/usr/bin/env python3
"""Extract the merge-server goal (合服目标, Config.HolidayMergeGoalData): the score every box needs (in thousandths of the base score),
what it holds, and the table that makes the server's score grow by itself (a number of points every interval, some hours a day).

usage: extract_mergegoal.py LUA_DIR OUT_JSON
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
K = "Config.HolidayMergeGoalData."
m = dump(lua_dir, "holiday_merge_goal_data.lua", [K + n for n in ("data_const", "data_score_award", "data_interval")])
out = {"source": "restored client holiday_merge_goal_data.lua",
       "base_max_score": m[K + "data_const"]["base_max_score"]["val"],
       "awards": [{"id": a["id"], "count": a["count"], "items": a["award"]} for a in m[K + "data_score_award"]],
       "days": [{"day": r["id"], "interval": r["interval"], "pro": r["pro"], "hours": r["time"]} for r in m[K + "data_interval"]]}
json.dump(out, open(out_json, "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out["awards"]), "boxes;", len(out["days"]), "days")
