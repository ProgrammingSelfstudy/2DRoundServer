#!/usr/bin/env python3
"""Extract the gold (coin) market from the restored client's market_gold_data.lua: the four shop lists
(skill, formation, break, other) with price band, stock, purchase limit and level, plus the items that can
only be sold (hide / stable / change lists) and the sale factor.

usage: extract_market_gold.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local d = Config.MarketGoldData
local out = {}
local function row(v, catalg)
  return string.format('{"base_id":%d,"catalg":%d,"price":%d,"min":%d,"max":%d,"stock":%d,"limit_num":%d,"limit_type":%d,"open_lev":%d,"sort":%d}',
    v.base_id, v.catalg, v.init_price, v.min_price, v.max_price, v.init_stock, v.limit_num, v.limit_type, v.open_lev, v.sort)
end
local goods = {}
for _, name in ipairs({"data_skill_shop_list", "data_form_shop_list", "data_break_shop_list", "data_other_shop_list"}) do
  for _, v in ipairs(d[name]) do goods[#goods + 1] = row(v) end
end
local hide, stable, change = {}, {}, {}
for id, v in pairs(d.data_hide_sell_list) do hide[#hide + 1] = string.format('{"id":%d,"open_lev":%d,"price":%d}', id, v.open_lev, v.price) end
for id, v in pairs(d.data_stable_sell_list) do stable[#stable + 1] = string.format('{"id":%d,"open_lev":%d,"discount":%d}', id, v.open_lev, v.discount) end
for id, v in pairs(d.data_change_sell_list) do change[#change + 1] = string.format('{"id":%d,"exchange_id":%d}', id, v.exchange_id) end
io.write(string.format('{"market_level":%d,"item_sale":%s,"goods":[%s],"hide":[%s],"stable":[%s],"change":[%s]}',
  d.data_market_gold_cost.market_level.val, tostring(d.data_market_gold_cost.item_sale.val),
  table.concat(goods, ","), table.concat(hide, ","), table.concat(stable, ","), table.concat(change, ",")))
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/market_gold_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
d = json.loads(raw)
for k in ("hide", "stable", "change"):
    d[k].sort(key=lambda e: e["id"])
d["source"] = "restored client market_gold_data.lua"
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["goods"]), "goods;", len(d["hide"]), "hidden sells;", len(d["stable"]), "stable;", len(d["change"]), "change")
