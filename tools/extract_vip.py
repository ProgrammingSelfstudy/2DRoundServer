#!/usr/bin/env python3
"""Extract the VIP table of the restored client: Config.VipData.data_get_reward, per
VIP level (0-30) the VIP experience ("gold") a role needs to be that level.

usage: extract_vip.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/vip_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_get_reward_length = (\d+)", src).group(1))
a = src.index("Config.VipData.data_get_reward = {")
block = src[a:src.index("\nfunction Config.VipData.data_get_reward_fun", a)] if "data_get_reward_fun" in src[a:] else src[a:]
exp = {}
for m in re.finditer(r"\n\t\t?gold = (\d+),\n\t\tlabel = \"[^\"]*\",\n\t\tlev = (\d+),", block):
    exp[int(m.group(2))] = int(m.group(1))
assert sorted(exp) == list(range(declared)), (sorted(exp), declared)
assert all(exp[i] <= exp[i + 1] for i in range(declared - 1)), "the experience must not fall with the level"

def pairs(body):
    return [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),\s*\}", body)]

# per level the gift of the VIP page: what it costs (price) and what it holds (items),
# each a list of [id, amount]
gifts = {}
for lm in re.finditer(r"\n\t(?:\[0\] = )?\{\n(.*?)\n\t\},", block, re.S):
    body = lm.group(1)
    lev = int(re.search(r"\n\t\tlev = (\d+),", "\n" + body).group(1))
    got = {}
    for key in ("price", "items"):
        km = re.search(r"\n\t\t" + key + r" = \{\n(.*?)\n\t\t\},", "\n" + body, re.S)
        got[key] = pairs(km.group(1))
    gifts[lev] = got
assert sorted(gifts) == list(range(declared)), sorted(gifts)
json.dump({"source": "restored client vip_data.lua Config.VipData.data_get_reward", "exp": [exp[i] for i in range(declared)],
           "price": [gifts[i]["price"] for i in range(declared)], "items": [gifts[i]["items"] for i in range(declared)]},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(declared, "levels;", [exp[i] for i in range(declared)])
