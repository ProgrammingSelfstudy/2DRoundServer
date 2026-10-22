#!/usr/bin/env python3
"""Extract the exchange shop goods (Config.ExchangeData.data_shop_exchage_*) from the
restored client: every entry that has an id, with the shop type it belongs to, the
item and amount, the payment (pay_type: an asset label, or an item id), the price
and the purchase limits per day / week / month (0 = none, 9999 = unlimited).
Tables without ids (hero, furniture, pet, suit) are left out.

usage: extract_shops.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
"""
import json, re, sys

src = open(sys.argv[1] + "/exchange_data.lua", encoding="utf-8").read()
goods = {}
seen_tables = []
for m in re.finditer(r"Config\.ExchangeData\.(data_shop_exchage_\w+) = \{", src):
    end = src.find("\nfunction", m.end())
    entries = re.findall(r"\n\t\{\n(.*?)\n\t\},", src[m.end():end], re.S)
    n = 0
    for e in entries:
        f = dict(re.findall(r'\n?\t\t(\w+) = ("(?:[^"\\]|\\.)*"|-?\d+),', e))
        if "id" not in f or "price" not in f:
            continue
        pay = f["pay_type"].strip('"')
        role_lv = [int(b) for a, b in re.findall(r'\{\s*"lv",\s*(\d+)\s*\}', e) if a == "lv"]
        g = {"type": int(f["type"]), "item": int(f["item_bid"]), "num": int(f["item_num"]),
             "pay": int(pay) if pay.isdigit() else pay, "price": int(f["price"]),
             "day": int(f.get("limit_day", 0)), "week": int(f.get("limit_week", 0)), "month": int(f.get("limit_month", 0))}
        gid = int(f["id"])
        if gid in goods:
            assert goods[gid] == g, ("id used twice with different content", gid, m.group(1))
            continue
        goods[gid] = g
        n += 1
    seen_tables.append((m.group(1), n))
json.dump({"source": "restored client exchange_data.lua data_shop_exchage_*", "goods": {str(k): v for k, v in sorted(goods.items())}},
          open(sys.argv[2], "w"), separators=(",", ":"))
print(len(goods), "goods;", seen_tables)
