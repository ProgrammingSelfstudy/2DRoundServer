#!/usr/bin/env python3
"""Extract the achievements ("feats", Config.FeatData.data_get) from the restored client:
per id the name, description, the one progress entry (cli_label names the game event,
target is its parameter, target_val the goal) and the rewards ([asset or item id, amount]).

usage: extract_feats.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/feat_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_get_length = (\d+)", src).group(1))
blk = src[src.index("Config.FeatData.data_get = {"):]
out = {}
for m in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", blk, re.S):
    b = m.group(2)
    prog = re.findall(r'cli_label = "(\w+)",\s+target = (\d+),\s+target_val = (\d+),', b)
    assert len(prog) == 1, (m.group(1), prog)
    rew = re.search(r"commit_rewards = \{(.*?)\n\t\t\},?\n", b + "\n", re.S)
    out[int(m.group(1))] = {
        "name": re.search(r'\tname = "(.*?)",', b).group(1), "desc": re.search(r'\tdesc = "(.*?)",', b).group(1),
        "hide": int(re.search(r"\thide = (\d+),", b).group(1)),
        "label": prog[0][0], "target": int(prog[0][1]), "target_val": int(prog[0][2]),
        "rewards": [[int(a), int(c)] for a, c in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", rew.group(1))] if rew else []}
assert len(out) == declared, (len(out), declared)
assert all(f["rewards"] for f in out.values()), "a feat without a reward"
json.dump({"source": "restored client feat_data.lua Config.FeatData.data_get", "feats": {str(k): v for k, v in sorted(out.items())}},
          open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(out), "feats")
