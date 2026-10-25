#!/usr/bin/env python3
"""Extract the crystal-slot rules of the hero resonance ("共鸣", the 圣阵) from the restored
client's resonate_data.lua: the constants (free slots, slot limit, cool-down, the cost of ending
a cool-down, the star a hero needs) and data_cell_cost (what the n-th unlocked slot costs in items
or in gems).

usage: extract_resonate.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local d = Config.ResonateData
local function pairs_(t) local o = {} for _, e in ipairs(t or {}) do o[#o + 1] = string.format("[%d,%d]", e[1], e[2]) end return "[" .. table.concat(o, ",") .. "]" end
local c = d.data_const
local rows = {}
for i, v in ipairs(d.data_cell_cost) do
  rows[#rows + 1] = string.format('{"count":%d,"item":%s,"gold":%s}', v.count, pairs_(v.loss1), pairs_(v.loss2))
end
local lv = {}
for i = 1, d.data_level_up_length do
  local v = d.data_level_up(i)
  if v then
    local at = {}
    for _, e in ipairs(v.attr) do at[#at + 1] = string.format('["%s",%d]', e[1], e[2]) end
    lv[#lv + 1] = string.format('{"lev":%d,"expend":%s,"attr":[%s],"limit":%d,"seconds":%d}', v.lev, pairs_(v.expend), table.concat(at, ","), v.limit_num, v.need_time)
  end
end
local sa = {}
for i = 1, d.data_star_attr_length do
  local v = d.data_star_attr(i)
  if v then sa[#sa + 1] = string.format('"%d":%d', v.star, v.rio) end
end
io.write(string.format('{"refine":%s,"alchemy":%d,"amp_stars":%d,"levels":[%s],"star_rio":{%s},', pairs_(c.single_refine_consume.val), c.alchemy_goods.val, c.amp_all_start_limit.val, table.concat(lv, ","), table.concat(sa, ",")) ..
string.format('"cell_init":%d,"cell_max":%d,"cool_minutes":%d,"flush":%s,"star":%d,"open_lev":%d,"cost":[%s]}',
  c.cell_init.val, c.cell_max.val, c.cell_cool_time.val, pairs_(c.cell_flush_expend.val), c.hero_star_condition.val, c.open_lev.val, table.concat(rows, ",")))
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/resonate_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
d = json.loads(raw)
d["source"] = "restored client resonate_data.lua (data_const, data_cell_cost)"
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(d["cell_init"], "free slot,", d["cell_max"], "at most,", d["cool_minutes"], "min cool-down, flush", d["flush"], "star >=", d["star"], "lev", d["open_lev"], "cost rows", len(d["cost"]))
