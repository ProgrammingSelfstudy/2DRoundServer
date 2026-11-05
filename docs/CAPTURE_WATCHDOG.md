# 抓包任务的独立停滞监控

`tools/capture_watchdog.py`只维护活动时间、目标聊天和恢复决策，不控制手机、不采集网络数据，也不自行中断或新建聊天。调用方使用独立监控聊天执行已经获得用户授权的恢复操作，采集聊天独占手机操作。

## 本地运行文件

以下文件包含设备、聊天、原包位置和实时进度，由本机任务建立并保留，不提交到Git：

- `docs/CAPTURE_ANALYSIS_HANDOFF.md`、`docs/CAPTURE_ANALYSIS_STATE.json`：离线协议解析交接及目标状态。
- `docs/DAILY_CAPTURE_HANDOFF.md`、`docs/DAILY_CAPTURE_WATCHDOG.json`：每日采集/归档交接及目标状态。

仓库只保留脱敏最小协议证据、校验摘要和结论。原包、完整解码、原图及完整动作日志留在仓库外私有目录。交接中必须记录当前授权、已领取/已消耗项目、当前页面、待办、采集连接/接收器和安全停止状态。

## 绑定与观察

先将`CAPTURE_THREAD_ID`设为已确认的唯一目标聊天ID，再绑定。仅当前授权允许采集时使用`--capture-active`；离线恢复不加此参数。

```sh
python3 tools/capture_watchdog.py --state docs/DAILY_CAPTURE_WATCHDOG.json bind \
  --thread-id "$CAPTURE_THREAD_ID" --host-id local --capture-active
```

监控调用方每30秒对单目标调用`wait_threads`，使用`timeoutMs: 0`，把完整返回保存为私有快照后执行：

```sh
python3 tools/capture_watchdog.py --state docs/DAILY_CAPTURE_WATCHDOG.json observe \
  --snapshot /private/tmp/2dround-daily-watchdog-snapshot.json
```

新turn、工具标记/状态或助手消息变化会重置最后活动时间；原包背景心跳增长不算AI活动。连续180秒无新活动才标记“疑似停滞”，这不是死锁证明。绑定新目标会增加generation并清除旧恢复决策；同一待恢复状态返回相同recovery_id，供调用方避免重复恢复。

| action | 调用方处理 |
|---|---|
| healthy / inactive | 静默，不重复报告无变化状态 |
| check_error | 检查工具或快照失败，不因监控失败终止采集 |
| finished | 核对完成/失败/中断结论、正常停抓和数据归档，删除本轮监控并通知 |
| recover / resume_recovery | 保存恢复记录，按下述顺序续接，不能直接启动第二个采集者 |

## 恢复顺序

1. 保存recovery_id、最后任务事件及交接进度；核对是否已有对应替代聊天。
2. 归档当前目标以中断，通过`wait_threads`确认旧turn中断或不活跃；不能退出整个Codex或使用`killall`。
3. 仅在用户已授权时创建恢复聊天。采集中先正常停止旧抓包连接并固定原包，复核奖励/免费次数后再开独立会话；离线归档阶段只继续未完成的离线步骤。
4. 新聊天ready后立即绑定其实际ID和host，更新交接及`work_stage`。已停抓时保留`capture_paused=true`，不能把恢复操作当成重新采集授权。

额外的购买、钻石/票券消耗、重复领取、社交及其他超出当前授权的行为不因恢复而获得授权。完成后核对PCAPdroid停止、接收器CLOSED、固定快照、接收器/转发清理及结论保存，再移除监控，避免旧任务持续被轮询。

状态机回归：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p test_capture_watchdog.py
```
