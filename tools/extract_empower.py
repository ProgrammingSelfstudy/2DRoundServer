#!/usr/bin/env python3
"""Extract the resonance empowerment (赋能) rules of resonate_data.lua: per target star the item and coin cost, the daily limit, how
long an empowered hero lasts (minutes) and the total of offering stars it needs.

usage: extract_empower.py LUA_DIR OUT_JSON
"""
import json, sys, os
sys.path.insert(0, os.path.dirname(__file__))
from luadump import dump

S = "Config.ResonateData."
m = dump(sys.argv[1], "resonate_data.lua", [S + "data_star_cost", S + "data_const"])
cost = {k: {"item": v["loss1"], "coin": v["loss2"]} for k, v in m[S + "data_star_cost"].items()}
c = m[S + "data_const"]
out = {"source": "restored client resonate_data.lua", "cost": cost, "day_limit": c["empowerment_day_limit"]["val"],
       "span": c["empowerment_span"]["val"], "condition": c["empowerment_condition"]["val"]}
assert len(cost) == 20 and out["day_limit"] == 5
json.dump(out, open(sys.argv[2], "w"), separators=(",", ":"))
print(out["day_limit"], out["span"], out["condition"], len(cost))
