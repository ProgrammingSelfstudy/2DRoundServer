#!/usr/bin/env python3
"""Extract the advanced-trial tasks (Config.RoomFeatData.data_exp_info) from the restored client:
per id the name, description, hide flag, level to show at, type and the rewards ([asset or item id, amount]).
The table has no progress targets; the server keeps its own conditions in exptask.go.

usage: extract_exptasks.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/room_feat_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_exp_info_length = (\d+)", src).group(1))
blk = src[src.index("Config.RoomFeatData.data_exp_info = {"):src.index("function Config.RoomFeatData.data_exp_info_fun")]
out = {}
for m in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", blk, re.S):
    b = m.group(2)
    rew = re.search(r"commit_rewards = \{(.*?)\n\t\t\},?\n", b + "\n", re.S)
    out[int(m.group(1))] = {
        "name": re.search(r'\tname = "(.*?)",', b).group(1), "desc": re.search(r'\tdesc = "(.*?)",', b).group(1),
        "hide": int(re.search(r"\thide = (\d+),", b).group(1)), "lev": int(re.search(r"\tlev = (\d+),", b).group(1)),
        "type": int(re.search(r"\ttype = (\d+),", b).group(1)),
        "rewards": [[int(a), int(c)] for a, c in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", rew.group(1))] if rew else []}
# the client declares 117 but lists 116
assert len(out) in (declared, declared - 1), (len(out), declared)
json.dump({"source": "restored client room_feat_data.lua Config.RoomFeatData.data_exp_info", "tasks": {str(k): v for k, v in sorted(out.items())}},
          open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out), "tasks")
