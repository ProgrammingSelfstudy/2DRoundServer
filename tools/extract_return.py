#!/usr/bin/env python3
"""Extract the hero return ("回退", back to 10 stars) rules from the restored client's
partner_data.lua: the constants (cost, target star / level / stage, refund rate, the stars that
may return) and data_partner_return (per star and camp: the copies of the hero and the
materials it gives back).

usage: extract_return.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local d = Config.PartnerData
local function pairs_(t) local o = {} for _, e in ipairs(t) do o[#o + 1] = string.format("[%d,%d]", e[1], e[2]) end return "[" .. table.concat(o, ",") .. "]" end
local c = d.data_partner_const
local rows = {}
for k, v in pairs(d.data_partner_return) do
  rows[#rows + 1] = string.format('"%s":{"star":%d,"camp":%d,"num":%d,"award":%s}', k, v.star, v.camp_type, v.num, pairs_(v.award))
end
local cond = {}
for _, s in ipairs(c.return_condition.val) do cond[#cond + 1] = tostring(s) end
io.write(string.format('{"cost":%s,"star":%d,"lev":%d,"stage":%d,"rate":%d,"stars":[%s],"rows":{%s}}',
  pairs_(c.return_cost.val), c.return_star.val, c.return_lv.val, c.return_stage.val, c.return_rate.val, table.concat(cond, ","), table.concat(rows, ",")))
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/partner_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
d = json.loads(raw)
d["source"] = "restored client partner_data.lua (return_* constants, data_partner_return)"
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["rows"]), "rows; cost", d["cost"], "to star", d["star"], "lev", d["lev"], "stage", d["stage"], "rate", d["rate"])
