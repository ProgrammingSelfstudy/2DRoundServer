-- Wire-format probe. Sends KNOWN values through the native SmartSocket to a
-- local mock server so the byte layout of PackData can be read off directly.
local function trace(s)
    local f = io.open(cc.FileUtils:getInstance():getWritablePath() .. 'probe.txt', 'a')
    if f then f:write(tostring(s) .. '\n'); f:close() end
end
DEBUG = 1
CC_USE_FRAMEWORK = false
CC_SHOW_FPS = false
CC_DISABLE_GLOBAL = false
CC_DESIGN_RESOLUTION = {width = 720, height = 1280, autoscale = 'FIXED_WIDTH'}

local HOST, PORT = '127.0.0.1', 8800
local ok, err = xpcall(function()
    local director = cc.Director:getInstance()
    local scene = cc.Scene:create()
    local lbl = cc.Label:createWithSystemFont('wire probe: starting', 'sans-serif', 26)
    local size = director:getVisibleSize()
    lbl:setPosition(size.width / 2, size.height / 2)
    scene:addChild(lbl)
    director:runWithScene(scene)
    local function say(s) trace(s); lbl:setString(s) end

    local proto = require('mmtZgQAwkKz4pG9sW05wVoadNxAlGKg9.net.proto_mate')
    local sm = cc.SmartSocket:getInstance()
    sm:loadSendMateData(proto.send)
    sm:loadRecvMateData(proto.recv)
    say('proto loaded')
    trace('Start -> ' .. tostring(sm:Start()))
    cc.Director:getInstance():getScheduler():scheduleScriptFunc(function() sm:Update() end, 0, false)

    local netid
    local tests = {
        {1197, {sign = 'ABCD', idx = 0x01020304, code = 0x0506, time = 0x0708090A}},
        {1164, {args = {{key = 'k1', val = 'v1'}, {key = 'k2', val = 'v2v'}}}},
        {1164, {args = {}}},
        {10191, {sex = 1, name = '', career = 0x0102, playform = 'p'}},
        {10192, {rid = 0x11223344, srv_id = 'a_1'}},
        {1169, {}},
    }
    function OnNetAsyncConnected(a, b)
        trace('CONNECTED ' .. tostring(a) .. ' ' .. tostring(b))
        netid = b or a
        local i = 0
        cc.Director:getInstance():getScheduler():scheduleScriptFunc(function()
            i = i + 1
            local t = tests[i]
            if not t then return end
            local sent = sm:Send(netid, t[1], t[2])
            say('sent ' .. t[1] .. ' -> ' .. tostring(sent))
        end, 0.5, false)
    end
    local function dump(v, ind)
        if type(v) ~= 'table' then return tostring(v) end
        local parts = {}
        for k, x in pairs(v) do parts[#parts + 1] = tostring(k) .. '=' .. dump(x) end
        table.sort(parts)
        return '{' .. table.concat(parts, ', ') .. '}'
    end
    function OnNetRecv(cmd, id, extra)
        local okg, tbl, good = pcall(function() return sm:GetRecv(cmd) end)
        trace('RECV cmd=' .. tostring(cmd) .. ' id=' .. tostring(id) .. ' ok=' .. tostring(okg) .. ' good=' .. tostring(good) .. ' ' .. dump(tbl))
    end
    function OnNetDisconnect(id) say('disconnected ' .. tostring(id)) end

    local sched = cc.Director:getInstance():getScheduler()
    local started = false
    local sid
    sid = sched:scheduleScriptFunc(function()
        if started then return end
        started = true
        sched:unscheduleScriptEntry(sid)
        netid = sm:Connect(HOST, PORT, 3000)
        say('Connect -> ' .. tostring(netid))
        local i = 0
        local tid
        tid = sched:scheduleScriptFunc(function()
            i = i + 1
            local t = tests[i]
            if not t then sched:unscheduleScriptEntry(tid); say('all sent'); return end
            local sent = sm:Send(netid, t[1], t[2])
            say('sent ' .. t[1] .. ' -> ' .. tostring(sent))
        end, 0.5, false)
    end, 1.0, false)
end, debug.traceback)
if not ok then trace('ERROR ' .. tostring(err)) end
