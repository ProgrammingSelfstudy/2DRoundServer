#!/usr/bin/env python3
"""Extract the guild rules from the restored client's guild_data.lua: the constants (what creating,
renaming, recruiting cost, cool-downs, limits), the guild level table (exp and member limit), the
assistant limit per guild level, the three donations and the donation boxes.

usage: extract_guild.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local d = Config.GuildData
local function pairs_(t) local o = {} for _, e in ipairs(t or {}) do o[#o + 1] = string.format("[%d,%d]", e[1], e[2]) end return "[" .. table.concat(o, ",") .. "]" end
local consts = {}
for k, v in pairs(d.data_const) do
  if type(v.val) == "number" then consts[#consts + 1] = string.format('"%s":%d', k, v.val) end
end
local levs = {}
for i, v in ipairs(d.data_guild_lev) do levs[#levs + 1] = string.format('{"lev":%d,"exp":%d,"num":%d}', v.lev, v.exp, v.num) end
local posts = {}
for k, v in pairs(d.data_post) do posts[#posts + 1] = string.format('"%s":%d', k, v.num) end
local donates = {}
for i, v in ipairs(d.data_donate) do donates[#donates + 1] = string.format('{"id":%d,"name":%q,"loss":%s,"gain":%s}', v.id, v.name, pairs_(v.loss), pairs_(v.gain)) end
local boxes = {}
for i, v in ipairs(d.data_donate_box) do boxes[#boxes + 1] = string.format('{"id":%d,"val":%d,"rewards":%s}', i, v.box_val, pairs_(v.rewards)) end
local pw = {}
for _, v in ipairs(d.data_const.guild_limit_power_choice.val) do pw[#pw + 1] = tostring(v) end
io.write(string.format('{"const":{%s},"levels":[%s],"assistants":{%s},"donate":[%s],"boxes":[%s],"power_choices":[%s]}',
  table.concat(consts, ","), table.concat(levs, ","), table.concat(posts, ","), table.concat(donates, ","), table.concat(boxes, ","), table.concat(pw, ",")))
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/guild_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
d = json.loads(raw)
d["source"] = "restored client guild_data.lua (data_const, data_guild_lev, data_post, data_donate, data_donate_box)"
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["const"]), "constants;", len(d["levels"]), "levels;", len(d["assistants"]), "assistant limits;", len(d["donate"]), "donations;", len(d["boxes"]), "boxes")
