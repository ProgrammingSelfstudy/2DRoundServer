#!/usr/bin/env python3
"""Dump tables of the restored client's Lua config files as JSON, for the extract_*.py tools.

usage as a module:  from luadump import dump;  dump(lua_dir, "adventure_data.lua", ["Config.AdventureData.data_map"])
usage on the command line:  luadump.py LUA_DIR FILE.lua TABLE [TABLE...]   (prints a JSON object {TABLE: value})

A Lua table with the keys 1..n becomes a list, any other table an object (number keys as strings), an empty table
an empty list. Rows that the client keeps as strings ("{1, 2, ...}", the `*_table` tables) are evaluated first.
Functions are dropped. Needs a LuaJIT on the PATH.
"""
import json, subprocess, sys

LUA = r'''
TI18N = function(s) return s end
Config = {}
local file = arg[1]
local names = {}
for i = 2, #arg do names[#names + 1] = arg[i] end
dofile(file)
local out = {}
local esc = {['"'] = '\\"', ['\\'] = '\\\\', ['\n'] = '\\n', ['\r'] = '\\r', ['\t'] = '\\t'}
local function str(s)
  return '"' .. s:gsub('[%c"\\]', function(c) return esc[c] or string.format("\\u%04x", c:byte()) end) .. '"'
end
local function ser(v, evalrow)
  local t = type(v)
  if t == "number" then
    if v ~= v or v == math.huge or v == -math.huge then return "null" end
    if v == math.floor(v) and math.abs(v) < 2^53 then return string.format("%d", v) end
    return string.format("%.17g", v)
  elseif t == "boolean" then return tostring(v)
  elseif t == "string" then
    if evalrow and v:sub(1, 1) == "{" then
      local f = loadstring("return " .. v)
      if f then return ser(f(), false) end
    end
    return str(v)
  elseif t == "table" then
    local n = 0
    for _ in pairs(v) do n = n + 1 end
    if n == 0 then return "[]" end
    local arr = true
    for i = 1, n do if v[i] == nil then arr = false break end end
    local parts = {}
    if arr then
      for i = 1, n do parts[#parts + 1] = ser(v[i], evalrow) end
      return "[" .. table.concat(parts, ",") .. "]"
    end
    local keys = {}
    for k in pairs(v) do keys[#keys + 1] = k end
    table.sort(keys, function(a, b)
      if type(a) == type(b) then return a < b end
      return tostring(a) < tostring(b)
    end)
    for _, k in ipairs(keys) do
      local x = v[k]
      if type(x) ~= "function" then parts[#parts + 1] = str(tostring(k):gsub("%.0$", "")) .. ":" .. ser(x, evalrow) end
    end
    return "{" .. table.concat(parts, ",") .. "}"
  end
  return "null"
end
for _, name in ipairs(names) do
  local v = _G
  for part in name:gmatch("[^.]+") do v = v and v[part] end
  out[#out + 1] = str(name) .. ":" .. ser(v, name:match("_table$") ~= nil)
end
io.write("{" .. table.concat(out, ",") .. "}")
'''


def dump(lua_dir, file, tables):
    path = file if file.startswith("/") else lua_dir + "/" + file
    p = subprocess.run(["luajit", "-", path] + list(tables), input=LUA, capture_output=True, text=True)
    if p.returncode != 0:
        raise SystemExit("luajit failed: " + p.stderr[:2000])
    return json.loads(p.stdout)


if __name__ == "__main__":
    print(json.dumps(dump(sys.argv[1], sys.argv[2], sys.argv[3:]), ensure_ascii=False))
