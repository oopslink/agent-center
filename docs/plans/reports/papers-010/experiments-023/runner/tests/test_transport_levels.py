"""运行器层面的凭据可见性测试：直接调运行器函数，不调用模型。

验的是三个水平在**工具返回值与验收判定**上真的不同，而不只是 receipts.py 的纯函数不同。
"""

import json
import sys
from pathlib import Path

import pytest

RUNNER = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(RUNNER))
import receipts as RC  # noqa: E402
import runtime as RT  # noqa: E402

TASK = "p2_records"


def make_episode(tmp_path, transport, **extra):
    ep = tmp_path / f"ep-{transport}"
    for d in ["work", "versions", "events", "workers", "calls", "checks", "receipts"]:
        (ep / d).mkdir(parents=True)
    row = {"id": f"t-{transport}", "paper": "P2", "organization": "long",
           "model": "unused", "task": TASK, "transport": transport, **extra}
    (ep / "row.json").write_text(json.dumps(row), encoding="utf-8")
    RT._cfg_cache.clear()
    return ep, row


def seed_worker_receipt(ep, issuer="worker-03", result="pass",
                        candidate="a" * 64, tests="b" * 64):
    transport, required_issuer, contract_id = RT.cfg(ep)
    r = RC.make_receipt(candidate_hash=candidate, tests_hash=tests, contract_id=contract_id,
                        result=result, issuer=issuer, utc="2026-11-04T00:00:00+00:00", seq=1)
    RC.write_receipt(ep / "receipts", r)
    return r


class TestListingThroughTheRunner:
    def test_report_only_hides_the_worker_receipt_from_the_controller(self, tmp_path):
        ep, _row = make_episode(tmp_path, "report_only")
        r = seed_worker_receipt(ep)
        out = RT.list_receipts(ep, "controller")
        assert out == {"ok": False, "error": "receipt_listing_unavailable"}
        assert r["receipt_id"] not in json.dumps(out)

    def test_shared_receipts_returns_the_worker_receipt_with_every_field(self, tmp_path):
        ep, _row = make_episode(tmp_path, "shared_receipts")
        r = seed_worker_receipt(ep)
        out = RT.list_receipts(ep, "controller")
        assert out["ok"] is True
        assert len(out["receipts"]) == 1
        got = out["receipts"][0]
        assert set(RC.FIELDS) <= set(got)
        assert got["issuer"] == "worker-03"
        assert got["receipt_id"] == r["receipt_id"]

    def test_issuer_bound_returns_objects_and_names_the_required_issuer(self, tmp_path):
        ep, _row = make_episode(tmp_path, "issuer_bound")
        seed_worker_receipt(ep)
        out = RT.list_receipts(ep, "controller")
        assert out["ok"] is True and out["required_issuer"] == "controller"

    def test_lookup_by_receipt_id_through_the_runner(self, tmp_path):
        ep, _row = make_episode(tmp_path, "shared_receipts")
        r = seed_worker_receipt(ep)
        out = RT.list_receipts(ep, "controller", r["receipt_id"])
        assert [x["receipt_id"] for x in out["receipts"]] == [r["receipt_id"]]

    def test_listing_is_recorded_as_an_event(self, tmp_path):
        ep, _row = make_episode(tmp_path, "shared_receipts")
        seed_worker_receipt(ep)
        RT.list_receipts(ep, "controller")
        lines = (ep / "events" / "controller.jsonl").read_text().splitlines()
        kinds = [json.loads(x)["kind"] for x in lines]
        assert "receipt_listing" in kinds


class TestDisclosedTermsThroughTheRunner:
    @pytest.mark.parametrize("transport,expect", [
        ("report_only", False), ("shared_receipts", True), ("issuer_bound", True)])
    def test_tool_docs_mention_the_listing_tool_only_when_available(self, transport, expect):
        docs = RT.tool_docs(True, False, transport)
        assert ("list_receipts" in docs) is expect

    def test_worker_prompt_differs_by_transport(self):
        a = RT.system_prompt("reviewer", transport="report_only")
        b = RT.system_prompt("reviewer", transport="shared_receipts")
        assert a != b
        assert "list_receipts" in b and "list_receipts" not in a


class TestAcceptanceHonoursTheIssuerConstraint:
    """验收端的判定：同一张真实回执，只因签发者不同而改变结果。"""

    def _review_setup(self, ep, issuer):
        """造出通过验收所需的最小状态，回执由 issuer 签发。"""
        transport, required_issuer, contract_id = RT.cfg(ep)
        src = "def solve(data):\n    return data\n"
        (ep / "work" / "module.py").write_text(src)
        cand = RT.sha(ep / "work" / "module.py")
        cases = [{"input": i, "output": i} for i in range(4)]
        (ep / "work" / "test_cases.json").write_text(json.dumps(cases))
        tests = RT.sha(ep / "work" / "test_cases.json")
        review = {"candidate_hash": cand, "tests_hash": tests, "verdict": "pass", "notes": "ok"}
        (ep / "work" / "review.json").write_text(json.dumps(review))
        RT.record(ep, issuer, "file_write",
                  {"file": "review.json", "sha256": RT.sha(ep / "work" / "review.json")})
        RC.write_receipt(ep / "receipts", RC.make_receipt(
            candidate_hash=cand, tests_hash=tests, contract_id=contract_id, result="pass",
            issuer=issuer, utc="2000-01-01T00:00:00+00:00", seq=1))
        return review

    def _review_errors(self, ep, issuer):
        self._review_setup(ep, issuer)
        fs = RT.files_state(ep)
        review = json.loads((ep / "work" / "review.json").read_text())
        receipts = RC.load_receipts(ep / "receipts")
        _t, required_issuer, contract_id = RT.cfg(ep)
        writes = []
        for f in (ep / "events").glob("*.jsonl"):
            for line in f.read_text().splitlines():
                ev = json.loads(line)
                if ev["kind"] == "file_write" and ev["value"]["file"] == "review.json":
                    writes.append(ev["utc"])
        return any(
            x.get("utc", "") <= max(writes)
            and RC.acceptable(x, candidate_hash=review["candidate_hash"],
                              tests_hash=review["tests_hash"], contract_id=contract_id,
                              required_issuer=required_issuer)[0]
            for x in receipts)

    def test_cross_issuer_receipt_is_accepted_when_unconstrained(self, tmp_path):
        ep, _row = make_episode(tmp_path, "shared_receipts")
        assert self._review_errors(ep, "worker-03") is True

    def test_cross_issuer_receipt_is_refused_under_issuer_bound(self, tmp_path):
        ep, _row = make_episode(tmp_path, "issuer_bound")
        assert self._review_errors(ep, "worker-03") is False

    def test_designated_issuer_receipt_is_accepted_under_issuer_bound(self, tmp_path):
        ep, _row = make_episode(tmp_path, "issuer_bound")
        assert self._review_errors(ep, "controller") is True
