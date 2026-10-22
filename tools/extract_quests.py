#!/usr/bin/env python3
"""Extract the daily quests (Config.QuestData.data_get) from the restored client:
per quest id the name, required role level, the progress label ("evt_..." names the
game event that counts), its target parameter and target value, and the rewards
(currency or item id, amount; asset 9 is the activity points).

usage: extract_quests.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/quest_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_get_length = (\d+)", src).group(1))
blk = src[src.index("Config.QuestData.data_get = {"):src.index("Config.QuestData.data_progress_lable_length")]
quests = {}
for m in re.finditer(r"\n\t\[(\d+)\] = \{\n(.*?)\n\t\},", blk, re.S):
    b = m.group(2)
    prog = re.findall(r'cli_label = "(\w+)",\s+target = (\d+),\s+target_val = (\d+)', b)
    assert len(prog) == 1, (m.group(1), prog)  # every daily quest has exactly one progress entry
    rew = re.search(r"commit_rewards = \{(.*?)\n\t\t\},", b, re.S).group(1)
    quests[int(m.group(1))] = {
        "name": re.search(r'\tname = "(.*?)",', b).group(1), "lev": int(re.search(r"\tlev = (\d+),", b).group(1)),
        "label": prog[0][0], "target": int(prog[0][1]), "target_val": int(prog[0][2]),
        "rewards": [[int(a), int(c)] for a, c in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", rew)]}
assert len(quests) == declared, (len(quests), declared)
json.dump({"source": "restored client quest_data.lua Config.QuestData.data_get", "quests": {str(k): v for k, v in sorted(quests.items())}},
          open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(quests), "daily quests;", sorted({q["label"] for q in quests.values()}))
