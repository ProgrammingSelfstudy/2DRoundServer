#!/usr/bin/env python3
"""Extract the compose recipes (Config.ItemProductData.data_product_data) from the
restored client: target base id -> need_items [[id, amount]] and loss [[id, amount]]
(extra cost, a currency or item; empty in this client).

usage: extract_compose.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/item_product_data.lua", encoding="utf-8").read()
declared = int(re.search(r"data_product_data_length = (\d+)", src).group(1))
start = src.index("Config.ItemProductData.data_product_data = {")
block = src[start:src.index("\nfunction Config.ItemProductData.data_product_data_fun", start)]
pairs = lambda body: [[int(a), int(b)] for a, b in re.findall(r"\{\s*(\d+),\s*(\d+),?\s*\}", body)]
recipes = {}
for m in re.finditer(r"\t\[(\d+)\] = \{\n\t\tbid = (\d+),.*?need_items = \{(.*?)\n\t\t\},\n\t\tloss = \{(.*?)\},\n\t\},?\n", block, re.S):
    assert m.group(1) == m.group(2)
    need = pairs(m.group(3))
    assert need, ("recipe without need_items", m.group(1))
    recipes[int(m.group(1))] = {"need": need, "loss": pairs(m.group(4))}
assert len(recipes) == declared, (len(recipes), declared)
json.dump({"source": "restored client item_product_data.lua data_product_data", "recipes": {str(k): v for k, v in sorted(recipes.items())}},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(recipes)
