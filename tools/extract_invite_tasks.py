#!/usr/bin/env python3
"""Extract the invite-code reward steps (Config.InviteCodeData.data_tesk_list) from
the restored client: for each step the condition on an invited player (their
power or VIP level), how many times it can be claimed, and the reward as a list
of [id, amount].

usage: extract_invite_tasks.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/invite_code_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_tesk_list_length = (\d+)", src).group(1))
start = src.index("Config.InviteCodeData.data_tesk_list = {")
end = src.index("Config.InviteCodeData.data_return_list", start)
block = src[start:end]
steps = []
for m in re.finditer(r'desc = "([^"]*)",\s*id = (\d+),\s*num = (\d+),\s*sort = (\d+),\s*conds = \{(.*?)\},\s*items = \{(.*?)\n\t\t\},', block, re.S):
    conds = re.findall(r'"(\w+)",\s*(\d+)', m.group(5))
    items = [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+)", m.group(6))]
    assert len(conds) == 1 and conds[0][0] in ("power", "vip"), ("unexpected condition", m.group(2), conds)
    assert items, ("a step without a reward", m.group(2))
    steps.append({"id": int(m.group(2)), "max": int(m.group(3)), "cond": [conds[0][0], int(conds[0][1])], "items": items})
assert len(steps) == declared, (len(steps), declared)  # a step the regex missed must not go unnoticed
assert [s["id"] for s in steps] == list(range(1, declared + 1))
json.dump({"source": "restored client invite_code_data.lua Config.InviteCodeData.data_tesk_list", "steps": steps},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(len(steps), "steps;", [(s["id"], s["cond"]) for s in steps])
