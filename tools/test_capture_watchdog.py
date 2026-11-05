import unittest

from capture_watchdog import bind, observe


def snapshot(tool="tool-1", turn_status="inProgress", status="active"):
    return {"polls": [{"thread": {"id": "worker", "status": {"type": status}},
                       "latestTurn": {"id": "turn-1", "status": turn_status},
                       "latestToolMarkerId": tool,
                       "latestToolMarker": {"status": "completed"}}]}


class WatchdogTest(unittest.TestCase):
    def setUp(self):
        self.state, _ = observe(bind({}, "worker", "local", 0), snapshot(), 0)

    def test_three_minute_boundary(self):
        state, decision = observe(self.state, snapshot(), 179)
        self.assertEqual(decision["action"], "healthy")
        state, decision = observe(state, snapshot(), 180)
        self.assertEqual(decision["action"], "recover")
        self.assertEqual(state["phase"], "recovery_pending")

    def test_new_tool_resets_timer(self):
        state, decision = observe(self.state, snapshot("tool-2"), 180)
        self.assertEqual(decision["action"], "healthy")
        self.assertEqual(state["last_activity_at"], 180)

    def test_pending_recovery_is_idempotent(self):
        state, first = observe(self.state, snapshot(), 180)
        state, second = observe(state, snapshot(turn_status="interrupted"), 210)
        self.assertEqual(second["action"], "resume_recovery")
        self.assertEqual(first["recovery_id"], second["recovery_id"])

    def test_completion_does_not_restart(self):
        state, decision = observe(self.state, snapshot(turn_status="completed"), 180)
        self.assertEqual(decision["action"], "finished")
        self.assertEqual(state["phase"], "completed")

    def test_snapshot_error_does_not_restart(self):
        state, decision = observe(self.state, {"polls": [], "errors": [{}]}, 180)
        self.assertEqual(decision["action"], "check_error")
        self.assertEqual(state, self.state)

    def test_new_target_resets_pending_recovery(self):
        state, _ = observe(self.state, snapshot(), 180)
        new = bind(state, "replacement", "local", 200)
        self.assertEqual(new["phase"], "monitoring")
        self.assertEqual(new["previous_thread_id"], "worker")
        self.assertEqual(new["generation"], 1)

    def test_live_capture_binding_preserves_new_authorization(self):
        state = bind(self.state, "daily-worker", "local", 200, capture_active=True)
        self.assertFalse(state["capture_paused"])
        self.assertEqual(state["check_interval_seconds"], 30)
        self.assertEqual(state["stall_after_seconds"], 180)


if __name__ == "__main__":
    unittest.main()
