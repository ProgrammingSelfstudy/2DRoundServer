#!/usr/bin/env python3
"""Extract the daily sign-in table (Config.CheckinData.data_award) from the
restored client: for each month (1-12) the rewards of its sign-in days, each a
list of [id, amount] (a currency id or an item base id).

usage: extract_checkin.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/checkin_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_award_length = (\d+)", src).group(1))
start = src.index("Config.CheckinData.data_award = {")
end = src.index("Config.CheckinData.data_const_length", start)
block = src[start:end]
months = {}
for m in re.finditer(r"day = (\d+),\s*is_show = \d+,\s*month = (\d+),\s*rewards = \{(.*?)\n\t\t\t\},", block, re.S):
    day, month, body = int(m.group(1)), int(m.group(2)), m.group(3)
    rewards = [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", body)]
    assert rewards, ("a sign-in day without a reward", month, day)
    months.setdefault(month, {})[day] = rewards
assert sorted(months) == list(range(1, declared + 1)), sorted(months)
out = []
for month in range(1, declared + 1):
    days = months[month]
    assert sorted(days) == list(range(1, len(days) + 1)), ("days must be 1..N", month, sorted(days))  # nothing skipped
    out.append([days[d] for d in range(1, len(days) + 1)])
json.dump({"source": "restored client checkin_data.lua Config.CheckinData.data_award", "months": out},
          open(sys.argv[2], "w"), separators=(",", ":"))
print("days per month:", [len(m) for m in out])
