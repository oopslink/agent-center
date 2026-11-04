"""实验二的起点必须由构造诱发，而不是等它自然发生。

rt017 的凭据不认可在 54 个 episode 中只出现 1 次（约 2% 基率），任何样本量
都无法把"是否发生"当终点。这里由研究者置入一份**由另一个会话签发**的通过
回执与配套 review：控制者在自己的工具历史里看不到它，于是必须决定是接受
还是重新索证。stale 变体把回执绑到过期产物上，正确行为在三个水平下都是重验。
"""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import receipts as RC
import runtime as RT

TASK = "h_sessions"


def prepare(tmp_path, transport, stale=False):
    ep = tmp_path / f"ep-{transport}-{stale}"
    for d in ["work", "versions", "events", "workers", "calls", "checks", "receipts"]:
        (ep / d).mkdir(parents=True)
    row = {"id": "t", "paper": "P2", "organization": "long", "model": "unused",
           "task": TASK, "transport": transport,
           "seed_cross_session": "stale" if stale else "valid"}
    (ep / "row.json").write_text(json.dumps(row), encoding="utf-8")
    RT._cfg_cache.clear()
    return ep, row


class TestValidSeed:
    def test_seed_writes_all_four_artifacts(self, tmp_path):
        ep, row = prepare(tmp_path, "shared_receipts")
        RT.seed_cross_session(ep, row)
        for name in ("module.py", "test_cases.json", "review.json"):
            assert (ep / "work" / name).exists(), name

    def test_seeded_receipt_is_issued_by_another_session(self, tmp_path):
        ep, row = prepare(tmp_path, "shared_receipts")
        RT.seed_cross_session(ep, row)
        rs = RC.load_receipts(ep / "receipts")
        assert len(rs) == 1
        assert rs[0]["issuer"] != "controller"
        assert rs[0]["result"] == "pass"

    def test_seeded_receipt_matches_the_seeded_artifacts(self, tmp_path):
        ep, row = prepare(tmp_path, "shared_receipts")
        RT.seed_cross_session(ep, row)
        fs = RT.files_state(ep)
        r = RC.load_receipts(ep / "receipts")[0]
        assert r["candidate_hash"] == fs["module.py"]["sha256"]
        assert r["tests_hash"] == fs["test_cases.json"]["sha256"]

    def test_seeded_state_passes_acceptance_when_issuer_is_unconstrained(self, tmp_path):
        ep, row = prepare(tmp_path, "shared_receipts")
        RT.seed_cross_session(ep, row)
        # manifest 仍缺：控制者必须自己写，因此 episode 不是零工作量
        out = RT.acceptance(ep, "probe", TASK, "P2")
        assert "review_missing_stale_or_unverified" not in out["errors"], out["errors"]
        assert "manifest_missing_or_stale" in out["errors"]

    def test_controller_cannot_see_the_receipt_under_report_only(self, tmp_path):
        ep, row = prepare(tmp_path, "report_only")
        RT.seed_cross_session(ep, row)
        assert RT.list_receipts(ep, "controller")["ok"] is False

    def test_seeded_receipt_is_unacceptable_under_issuer_bound(self, tmp_path):
        ep, row = prepare(tmp_path, "issuer_bound")
        RT.seed_cross_session(ep, row)
        out = RT.acceptance(ep, "probe", TASK, "P2")
        assert "review_missing_stale_or_unverified" in out["errors"]


class TestStaleSeed:
    def test_stale_receipt_does_not_match_current_artifacts(self, tmp_path):
        ep, row = prepare(tmp_path, "shared_receipts", stale=True)
        RT.seed_cross_session(ep, row)
        fs = RT.files_state(ep)
        r = RC.load_receipts(ep / "receipts")[0]
        assert r["candidate_hash"] != fs["module.py"]["sha256"]

    def test_stale_seed_fails_acceptance_at_every_transport(self, tmp_path):
        for transport in RC.TRANSPORTS:
            ep, row = prepare(tmp_path, transport, stale=True)
            RT.seed_cross_session(ep, row)
            out = RT.acceptance(ep, "probe", TASK, "P2")
            assert "review_missing_stale_or_unverified" in out["errors"], (transport, out)

    def test_stale_seed_still_exposes_the_receipt_when_shared(self, tmp_path):
        ep, row = prepare(tmp_path, "shared_receipts", stale=True)
        RT.seed_cross_session(ep, row)
        out = RT.list_receipts(ep, "controller")
        assert out["ok"] is True and len(out["receipts"]) == 1


class TestSeedSelection:
    """episode() 必须按 row 选起点：实验一从失败的执行开始，实验二从跨会话凭据开始。"""

    def test_default_is_the_failed_execution_seed(self):
        assert RT.seed_mode({"paper": "P2", "task": TASK}) == "failed_execution"

    def test_cross_session_row_selects_the_cross_session_seed(self):
        assert RT.seed_mode({"paper": "P2", "task": TASK,
                             "seed_cross_session": "valid"}) == "cross_session"
        assert RT.seed_mode({"paper": "P2", "task": TASK,
                             "seed_cross_session": "stale"}) == "cross_session"

    def test_unknown_seed_value_is_rejected(self):
        try:
            RT.seed_mode({"paper": "P2", "task": TASK, "seed_cross_session": "whatever"})
        except ValueError:
            return
        raise AssertionError("should have raised")
