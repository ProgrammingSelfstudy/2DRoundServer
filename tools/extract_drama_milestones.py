#!/usr/bin/env python3
"""Extract the campaign milestone rewards (Config.DungeonData.data_drama_reward) from the
restored client: per reward id the dungeon that must have been reached (limit_id; the
client compares it with max_dun_id) and the rewards ([asset or item id, amount]).

usage: extract_drama_milestones.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/dungeon_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_drama_reward_length = (\d+)", src).group(1))
blk = src[src.index("Config.DungeonData.data_drama_reward = {"):src.index("Config.DungeonData.data_drama_show_reward_length")]
out = {}
for m in re.finditer(r"\n\t\{\n(.*?)\n\t\},?(?=\n)", blk, re.S):
    t = m.group(1)
    rid = int(re.search(r"\tid = (\d+),", t).group(1))
    items = [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", t.split("items = ")[1])]
    assert items and rid not in out, rid
    out[rid] = {"limit": int(re.search(r"limit_id = (\d+),", t).group(1)), "items": items}
assert len(out) == declared, (len(out), declared)
json.dump({"source": "restored client dungeon_data.lua data_drama_reward", "rewards": {str(k): v for k, v in sorted(out.items())}},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(len(out), "milestone rewards; biggest amount:", max(b for v in out.values() for a, b in v["items"]))
