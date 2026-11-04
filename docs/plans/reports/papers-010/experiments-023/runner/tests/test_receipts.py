"""三个凭据可见性水平的仪器测试。

被测的是"谁能看到哪张回执、哪张回执算有效"，不涉及模型调用。
"""

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import receipts as R  # noqa: E402

CAND = "a" * 64
TESTS = "b" * 64
CONTRACT = "p2-four-role-v1"


def reviewer_receipt(**over):
    base = dict(
        candidate_hash=CAND, tests_hash=TESTS, contract_id=CONTRACT,
        result="pass", issuer="worker-03", utc="2026-11-04T00:00:00+00:00", seq=1,
    )
    base.update(over)
    return R.make_receipt(**base)


class TestReceiptObject:
    def test_receipt_carries_every_required_field(self):
        r = reviewer_receipt()
        assert set(R.FIELDS) <= set(r), set(R.FIELDS) - set(r)
        assert r["issuer"] == "worker-03"
        assert r["contract_id"] == CONTRACT

    def test_receipt_id_is_stable_for_the_same_inputs(self):
        assert reviewer_receipt()["receipt_id"] == reviewer_receipt()["receipt_id"]

    def test_receipt_id_differs_when_candidate_differs(self):
        assert reviewer_receipt()["receipt_id"] != reviewer_receipt(candidate_hash="c" * 64)["receipt_id"]

    def test_round_trip_through_disk(self, tmp_path):
        r = reviewer_receipt()
        R.write_receipt(tmp_path, r)
        assert R.load_receipts(tmp_path) == [r]


class TestVisibility:
    def test_report_only_exposes_no_receipt_object(self):
        out = R.listing("report_only", [reviewer_receipt()])
        assert out["ok"] is False
        assert out["error"] == "receipt_listing_unavailable"
        assert "worker-03" not in json.dumps(out, ensure_ascii=False)

    def test_shared_receipts_exposes_full_objects(self):
        r = reviewer_receipt()
        out = R.listing("shared_receipts", [r])
        assert out["ok"] is True
        assert out["receipts"] == [r]
        assert out.get("required_issuer") is None

    def test_shared_receipts_supports_lookup_by_id(self):
        r = reviewer_receipt()
        out = R.listing("shared_receipts", [reviewer_receipt(candidate_hash="c" * 64), r],
                        receipt_id=r["receipt_id"])
        assert [x["receipt_id"] for x in out["receipts"]] == [r["receipt_id"]]

    def test_unknown_receipt_id_returns_empty_not_error(self):
        out = R.listing("shared_receipts", [reviewer_receipt()], receipt_id="nope")
        assert out["ok"] is True and out["receipts"] == []

    def test_issuer_bound_exposes_objects_and_names_the_required_issuer(self):
        out = R.listing("issuer_bound", [reviewer_receipt()], required_issuer="controller")
        assert out["ok"] is True
        assert out["required_issuer"] == "controller"


class TestAcceptability:
    def test_matching_receipt_is_acceptable_without_issuer_constraint(self):
        ok, why = R.acceptable(reviewer_receipt(), candidate_hash=CAND,
                               tests_hash=TESTS, contract_id=CONTRACT)
        assert ok, why

    def test_valid_receipt_from_other_issuer_is_rejected_under_issuer_bound(self):
        ok, why = R.acceptable(reviewer_receipt(), candidate_hash=CAND, tests_hash=TESTS,
                               contract_id=CONTRACT, required_issuer="controller")
        assert not ok
        assert why == "issuer_not_permitted"

    def test_designated_issuer_is_accepted_under_issuer_bound(self):
        ok, why = R.acceptable(reviewer_receipt(issuer="controller"), candidate_hash=CAND,
                               tests_hash=TESTS, contract_id=CONTRACT,
                               required_issuer="controller")
        assert ok, why

    @pytest.mark.parametrize("required_issuer", [None, "controller", "worker-03"])
    def test_stale_candidate_version_is_rejected_at_every_level(self, required_issuer):
        """无效回执：回执绑定的产物版本与当前产物不符。"""
        ok, why = R.acceptable(reviewer_receipt(), candidate_hash="c" * 64, tests_hash=TESTS,
                               contract_id=CONTRACT, required_issuer=required_issuer)
        assert not ok
        assert why == "candidate_version_mismatch"

    @pytest.mark.parametrize("required_issuer", [None, "controller", "worker-03"])
    def test_stale_tests_version_is_rejected_at_every_level(self, required_issuer):
        ok, why = R.acceptable(reviewer_receipt(), candidate_hash=CAND, tests_hash="d" * 64,
                               contract_id=CONTRACT, required_issuer=required_issuer)
        assert not ok
        assert why == "tests_version_mismatch"

    @pytest.mark.parametrize("required_issuer", [None, "controller", "worker-03"])
    def test_failed_receipt_is_never_acceptable(self, required_issuer):
        ok, why = R.acceptable(reviewer_receipt(result="fail"), candidate_hash=CAND,
                               tests_hash=TESTS, contract_id=CONTRACT,
                               required_issuer=required_issuer)
        assert not ok
        assert why == "result_not_pass"

    def test_wrong_contract_is_rejected(self):
        ok, why = R.acceptable(reviewer_receipt(), candidate_hash=CAND, tests_hash=TESTS,
                               contract_id="other-contract")
        assert not ok
        assert why == "contract_mismatch"


class TestDisclosedContract:
    def test_report_only_text_does_not_promise_a_listing_tool(self):
        text = R.contract_text("report_only")
        assert "list_receipts" not in text

    def test_shared_receipts_text_mentions_the_listing_tool(self):
        assert "list_receipts" in R.contract_text("shared_receipts")

    def test_issuer_bound_text_names_the_required_issuer(self):
        text = R.contract_text("issuer_bound", required_issuer="controller")
        assert "controller" in text
        assert "list_receipts" in text

    def test_unknown_transport_is_an_error(self):
        with pytest.raises(ValueError):
            R.contract_text("something_else")
