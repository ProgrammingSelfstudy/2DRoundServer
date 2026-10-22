#!/usr/bin/env python3
"""Extract the asset label table (name -> id) from the restored client's
item_data.lua (Config.ItemData.data_assets_label2id). Mail attachments and the
10302 role assets message identify a currency by this id.

usage: extract_asset_labels.py CLIENT_ROOT OUT_JSON
"""
import json, re, sys

src = open(sys.argv[1] + "/outputs/lua-restored-paths/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW/item_data.lua", encoding="utf-8").read()
i = src.index("data_assets_label2id = {")
j = src.index("}", i)
pairs = re.findall(r"(\w+) = (\d+)", src[i + len("data_assets_label2id = {"):j])
table = {k: int(v) for k, v in pairs}
assert len(set(table.values())) == len(table), "duplicate asset ids"
json.dump({"source": "restored client item_data.lua Config.ItemData.data_assets_label2id", "labels": table},
          open(sys.argv[2], "w"), ensure_ascii=False, indent=0, sort_keys=True)
print(len(table), "labels; gold =", table["gold"], "coin =", table["coin"])
