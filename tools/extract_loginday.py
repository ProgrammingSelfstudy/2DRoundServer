#!/usr/bin/env python3
"""Extract the eight-day or seven-day login rewards: per day the rewards ([item id, amount]).

usage: extract_loginday.py LUA_DIR OUT_JSON [--seven]
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import argparse, json, sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from luadump import dump

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("lua_dir")
parser.add_argument("out_json")
parser.add_argument("--seven", action="store_true")
args = parser.parse_args()
filename = "login_days_data.lua" if args.seven else "login_days_new_data.lua"
table = "Config.LoginDaysData.data_day" if args.seven else "Config.LoginDaysNewData.data_day"
m = dump(args.lua_dir, filename, [table])[table]
rows = sorted(m.values() if isinstance(m, dict) else m, key=lambda r: r["day"])
days = [r["rewards"] for r in rows]
assert len(days) == (7 if args.seven else 8) and all(days), days
json.dump({"source": f"restored client {filename}", "days": days}, open(args.out_json, "w"), ensure_ascii=False, separators=(",", ":"))
print("login days:", len(days))
