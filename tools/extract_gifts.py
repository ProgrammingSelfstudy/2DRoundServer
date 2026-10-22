#!/usr/bin/env python3
"""Extract the gift box table (Config.GiftData.data_gift_data) from the restored
client: for each gift id its rand_num (0 = every entry is given, n = n random
entries) and its items, a list of [id, amount] (a currency id or an item base id).
Gifts whose item list is empty in the client (their content lives only on the
original server) are kept out: they cannot be opened here.

usage: extract_gifts.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/gift_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_gift_data_length = (\d+)", src).group(1))
head = re.compile(r"^\t\[(\d+)\] = \{\n\t\tid = (\d+),\n\t\tname = \"(?:[^\"\\]|\\.)*\",\n\t\trand_num = (\d+),\n\t\tstat_num = \d+,\n\t\titems = \{(.*?)\},\n\t\tcli_items", re.M | re.S)
gifts, total = {}, 0
for key, id_, rand, body in head.findall(src):
    assert key == id_, (key, id_)
    total += 1
    items = [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", body)]
    if items:
        gifts[int(id_)] = {"rand": int(rand), "items": items}
assert total == declared, (total, declared)  # a gift the regex missed must not go unnoticed
json.dump({"source": "restored client gift_data.lua Config.GiftData.data_gift_data",
           "gifts": {str(k): v for k, v in sorted(gifts.items())}}, open(sys.argv[2], "w"), separators=(",", ":"))
print(total, "gifts,", len(gifts), "with content;", sum(1 for g in gifts.values() if g["rand"]), "random")
