#!/usr/bin/env python3
"""Extract the guild skill rules from the restored client's guild_skill_data.lua: the reset constants, the
ordinary (PvE) skills with their cost and the guild level they need, the groups ("heavens") of each career,
the PvP attributes per level and the PvP skill per level.

usage: extract_guild_skill.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH (the file is plain Lua tables).
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local d = Config.GuildSkillData
local function pairs_(t) local o = {} for _, e in ipairs(t or {}) do o[#o + 1] = string.format("[%d,%d]", e[1], e[2]) end return "[" .. table.concat(o, ",") .. "]" end
local consts = {}
for _, k in ipairs({"max_lv","pvp_max_lv","reset_consume_var1","reset_consume_var2","first_reset_consume","pvp_reset_consume_var1","pvp_reset_consume_var2","pvp_first_reset_consume"}) do
  consts[#consts + 1] = string.format('"%s":%d', k, d.data_const[k].val)
end
local ratio = pairs_(d.data_const.return_resource_ratio.val)
local skills = {}
for id in pairs(d.data_info_table) do
  local v = d.data_info(id)
  skills[#skills + 1] = string.format('{"id":%d,"group":%d,"seq":%d,"guild_lev":%d,"lev":%d,"loss":%s}', v.id, v.group_id, v.seq, v.guild_lev, v.lev, pairs_(v.loss))
end
local groups = {}
for k, v in pairs(d.data_group) do groups[#groups + 1] = string.format('{"career":%d,"id":%d,"seq":%d}', v.career, v.group_id, v.group_seq) end
local attrs = {}
for k in pairs(d.data_pvp_attr_info_table) do
  local v = d.data_pvp_attr_info(k)
  attrs[#attrs + 1] = string.format('{"id":%d,"lev":%d,"need_lev":%d,"loss":%s,"power":%d,"back":%s}', v.id, v.lev, v.need_lev, pairs_(v.loss), v.power, pairs_(v.return_res))
end
local maxl = {}
for id, n in pairs(d.data_pvp_attr_max_lev) do maxl[#maxl + 1] = string.format('"%d":%d', id, n) end
local pskills = {}
for k in pairs(d.data_pvp_skill_info_table) do
  local v = d.data_pvp_skill_info(k)
  pskills[#pskills + 1] = string.format('{"skill_id":%d,"career":%d,"lev":%d,"need_lev":%d,"loss":%s,"power":%d,"back":%s}', v.skill_id, v.career, v.lev, v.need_lev, pairs_(v.loss), v.power, pairs_(v.return_res))
end
io.write(string.format('{"const":{%s},"return_ratio":%s,"skills":[%s],"groups":[%s],"pvp_attrs":[%s],"pvp_attr_max":{%s},"pvp_skills":[%s]}',
  table.concat(consts, ","), ratio, table.concat(skills, ","), table.concat(groups, ","), table.concat(attrs, ","), table.concat(maxl, ","), table.concat(pskills, ",")))
'''
raw = subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/guild_skill_data.lua") + LUA],
                     capture_output=True, text=True, check=True).stdout
d = json.loads(raw)
d["skills"].sort(key=lambda e: e["id"])
d["groups"].sort(key=lambda e: e["id"])
d["pvp_attrs"].sort(key=lambda e: (e["id"], e["lev"]))
d["pvp_skills"].sort(key=lambda e: (e["career"], e["lev"]))
d["source"] = "restored client guild_skill_data.lua"
json.dump(d, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False, separators=(",", ":"))
print(len(d["skills"]), "skills;", len(d["groups"]), "groups;", len(d["pvp_attrs"]), "pvp attr levels;", len(d["pvp_skills"]), "pvp skill levels")
