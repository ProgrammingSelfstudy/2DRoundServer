#!/usr/bin/env python3
"""Persist decisions for the independent Codex capture-analysis heartbeat.

The scheduler calls this with a wait_threads snapshot every 30 seconds. This
program never controls the phone or kills processes. The heartbeat executes
the returned recovery action through supported Codex chat tools.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import tempfile
import time


DEFAULT_STATE = Path(__file__).resolve().parents[1] / "docs/CAPTURE_ANALYSIS_STATE.json"


def save(path, state):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as out:
        json.dump(state, out, ensure_ascii=False, indent=2)
        out.write("\n")
        temporary = out.name
    os.replace(temporary, path)


def bind(previous, thread_id, host_id, now, capture_active=False):
    return {
        "version": 1,
        "thread_id": thread_id,
        "host_id": host_id,
        "check_interval_seconds": 30,
        "stall_after_seconds": 180,
        "phase": "monitoring",
        "generation": previous.get("generation", -1) + 1,
        "previous_thread_id": previous.get("thread_id"),
        "bound_at": now,
        "last_activity_at": now,
        "fingerprint": None,
        "capture_paused": not capture_active,
    }


def unwrap(snapshot):
    if "polls" in snapshot:
        return snapshot
    for item in snapshot.get("content", []):
        if item.get("type") == "text":
            try:
                candidate = json.loads(item["text"])
            except (ValueError, KeyError):
                continue
            if "polls" in candidate:
                return candidate
    raise ValueError("Expected a wait_threads snapshot containing polls")


def observe(state, snapshot, now):
    state = dict(state)
    data = unwrap(snapshot)
    poll = next((p for p in data.get("polls", [])
                 if p.get("thread", {}).get("id") == state["thread_id"]), None)
    if poll is None:
        return state, {"action": "check_error", "reason": "Target snapshot unavailable; do not restart."}
    if state["phase"] == "recovery_pending":
        return state, {"action": "resume_recovery", "thread_id": state["thread_id"],
                       "recovery_id": state["recovery_id"]}
    turn = poll.get("latestTurn") or {}
    status = (poll["thread"].get("status") or {}).get("type")
    if turn.get("status") in {"completed", "failed", "interrupted"}:
        state.update(phase=turn["status"], last_checked_at=now)
        return state, {"action": "finished", "status": turn["status"]}
    if status != "active":
        state["last_checked_at"] = now
        return state, {"action": "inactive", "status": status}
    marker = poll.get("latestToolMarker") or {}
    message = poll.get("latestAssistantMessage") or {}
    activity = {"turn_id": turn.get("id"), "turn_status": turn.get("status"),
                "tool_id": poll.get("latestToolMarkerId"), "tool_status": marker.get("status"),
                "message_id": poll.get("latestAssistantMessageId"), "text": message.get("text")}
    if not any(activity.values()):
        return state, {"action": "check_error", "reason": "Activity markers missing; do not restart."}
    fingerprint = hashlib.sha256(json.dumps(activity, sort_keys=True).encode()).hexdigest()
    if fingerprint != state.get("fingerprint"):
        state.update(fingerprint=fingerprint, last_activity_at=now)
    state["last_checked_at"] = now
    silent = max(0, now - state["last_activity_at"])
    if silent >= state["stall_after_seconds"]:
        recovery_id = f'{state["thread_id"]}:{state["generation"]}:{int(now)}'
        state.update(phase="recovery_pending", suspected_stall_at=now, recovery_id=recovery_id)
        return state, {"action": "recover", "label": "疑似停滞", "silent_seconds": silent,
                       "thread_id": state["thread_id"], "recovery_id": recovery_id}
    return state, {"action": "healthy", "silent_seconds": silent}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state", type=Path, default=DEFAULT_STATE)
    parser.add_argument("--now", type=float, default=None, help="UTC epoch; normally use the clock")
    sub = parser.add_subparsers(dest="command", required=True)
    init = sub.add_parser("bind")
    init.add_argument("--thread-id", required=True)
    init.add_argument("--host-id", default="local")
    init.add_argument("--capture-active", action="store_true", help="This worker is authorized to collect new captures")
    check = sub.add_parser("observe")
    check.add_argument("--snapshot", type=Path, required=True)
    args = parser.parse_args()
    now = time.time() if args.now is None else args.now
    state = json.loads(args.state.read_text()) if args.state.exists() else {}
    if args.command == "bind":
        state = bind(state, args.thread_id, args.host_id, now, args.capture_active)
        result = {"action": "bound", "thread_id": args.thread_id, "generation": state["generation"]}
    else:
        if not state:
            parser.error("Bind a target before observing")
        state, result = observe(state, json.loads(args.snapshot.read_text()), now)
    save(args.state, state)
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
