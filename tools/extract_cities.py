#!/usr/bin/env python3
"""Extract the city ids a role may pick as its home (Config.RoleData.data_city_list of the restored client).

usage: extract_cities.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH.
"""
import json, subprocess, sys

LUA = r'''
Config = {}
dofile(arg[1])
local ids = {}
for _, province in ipairs(Config.RoleData.data_city_list) do for _, c in ipairs(province) do ids[#ids + 1] = tostring(c.city_id) end end
io.write("[" .. table.concat(ids, ",") .. "]")
'''
ids = json.loads(subprocess.run(["luajit", "-e", "arg={[1]=%r} " % (sys.argv[1] + "/role_data.lua") + LUA], capture_output=True, text=True, check=True).stdout)
json.dump({"source": "restored client role_data.lua data_city_list", "cities": sorted(ids)}, open(sys.argv[2], "w"), separators=(",", ":"))
print(len(ids), "cities")
