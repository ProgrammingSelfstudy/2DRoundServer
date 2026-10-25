#!/usr/bin/env python3
"""Extract the walkable grids of the ten planes maps (5 dungeons x 2 floors) from the restored client:
Config.MapBlock.data(map_id) (config_map_block.lua) is a list of rows of cells, and Config.Map[map_id] (config_map.lua) says
how many rows and columns the map has.

usage: extract_planes_maps.py LUA_DIR OUT_JSON
  LUA_DIR  .../outputs/readable-source/lua/ePoCrKQkWA3vpiOwu2oNfOh02MOeNheW
Needs a LuaJIT on the PATH.
"""
import json, subprocess, sys

lua_dir, out_json = sys.argv[1:3]
IDS = [d * 10000 + f * 100 + 1 for d in range(1, 6) for f in (1, 2)]
LUA = r'''
Config = {}
dofile(arg[1] .. "/config_map_block.lua")
dofile(arg[1] .. "/config_map.lua")
local out = {}
for i = 2, #arg do
  local id = tonumber(arg[i])
  local rows = Config.MapBlock.data(id)
  local m = Config.Map[id]
  local parts = {}
  for y, row in ipairs(rows or {}) do parts[#parts + 1] = "[" .. table.concat(row, ",") .. "]" end
  out[#out + 1] = string.format('"%d":{"cols":%d,"rows":%d,"width":%d,"height":%d,"tile_w":%d,"tile_h":%d,"block":[%s]}', id, m.cols, m.rows, m.width, m.height, m.tile_w, m.tile_h, table.concat(parts, ","))
end
io.write("{" .. table.concat(out, ",") .. "}")
'''
r = subprocess.run(["luajit", "-", lua_dir] + [str(i) for i in IDS], input=LUA, capture_output=True, text=True)
if r.returncode:
    raise SystemExit(r.stderr[:2000])
maps = json.loads(r.stdout)
json.dump({"source": "restored client config_map_block.lua, config_map.lua", "maps": maps}, open(out_json, "w"), separators=(",", ":"))
for k, m in maps.items():
    vals = {}
    for row in m["block"]:
        for v in row:
            vals[v] = vals.get(v, 0) + 1
    print(k, m["cols"], "x", m["rows"], "rows:", len(m["block"]), "cells/row:", len(m["block"][0]), vals)
