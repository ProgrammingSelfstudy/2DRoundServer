#!/usr/bin/env python3
"""Extract the eight-day login rewards (login_days_new_data.lua): per day the rewards ([item id, amount]).

usage: extract_loginday.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

lua_dir, out_json = sys.argv[1:3]
S = "Config.LoginDaysNewData."
m = dump(lua_dir, "login_days_new_data.lua", [S + "data_day"])[S + "data_day"]
rows = sorted(m.values() if isinstance(m, dict) else m, key=lambda r: r["day"])
days = [r["rewards"] for r in rows]
assert len(days) == 8 and all(days), days
json.dump({"source": "restored client login_days_new_data.lua", "days": days}, open(out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("login days:", len(days))
