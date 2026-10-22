#!/usr/bin/env python3
"""Extract the online-gift table (Config.MiscData.data_get_time_items) from the
restored client: for each step the online seconds needed and its reward, a list of
[id, amount] (a currency id or an item base id).

usage: extract_online_gifts.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/misc_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_get_time_items_length = (\d+)", src).group(1))
start = src.index("Config.MiscData.data_get_time_items = {")
end = src.index("\nfunction Config.MiscData.data_get_time_items_fun", start)
block = src[start:end]
steps = []
for m in re.finditer(r"index = (\d+),\s*time = (\d+),\s*items = \{(.*?)\n\t\t\},", block, re.S):
    idx, secs, body = int(m.group(1)), int(m.group(2)), m.group(3)
    items = [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", body)]
    assert items, ("a step without items", idx)
    steps.append({"index": idx, "time": secs, "items": items})
assert len(steps) == declared, (len(steps), declared)  # a step the regex missed must not go unnoticed
assert [s["index"] for s in steps] == list(range(1, declared + 1))
assert all(a["time"] < b["time"] for a, b in zip(steps, steps[1:])), "steps must be in time order"
json.dump({"source": "restored client misc_data.lua Config.MiscData.data_get_time_items", "steps": steps},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(len(steps), "steps;", [(s["time"], s["items"]) for s in steps])
