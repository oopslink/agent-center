# -*- coding: utf-8 -*-
"""凭据可见性与可接受性。

rt017 的运行器把测试回执写到 episode 的共享目录，但没有任何工具把回执对象
暴露出去：`inspect()` 只返回文件与 worker 状态，`run_tests()` 只返回本次调用
自己产生的回执。因此一个会话无法核对另一个会话签发的凭据——这是 rt017 中那次
停摆的结构条件。本模块把这个条件做成三个可操纵的水平：

- `report_only`     —— 保持 rt017 的行为：没有回执查询工具。
- `shared_receipts` —— 提供 `list_receipts`，可按 receipt_id 查到完整回执对象。
- `issuer_bound`    —— 同上，但合同声明只接受指定签发者的回执，验收端照此执行。

可接受性与可见性是两件事：无效回执（绑定的产物版本与当前产物不符、或结果不是
pass）在三个水平下都不可接受，这条反例用于确认"共享回执"不是让验收端变轻信。
"""

import hashlib
import json
from pathlib import Path

TRANSPORTS = ("report_only", "shared_receipts", "issuer_bound")
FIELDS = ("receipt_id", "candidate_hash", "tests_hash", "contract_id", "result", "issuer", "utc")


def make_receipt(*, candidate_hash, tests_hash, contract_id, result, issuer, utc, seq):
    """receipt_id 由内容决定，便于跨会话引用同一张回执而不依赖文件名。"""
    if result not in ("pass", "fail"):
        raise ValueError(f"result must be pass or fail, got {result!r}")
    payload = {
        "candidate_hash": candidate_hash,
        "tests_hash": tests_hash,
        "contract_id": contract_id,
        "result": result,
        "issuer": issuer,
        "seq": seq,
    }
    digest = hashlib.sha256(
        json.dumps(payload, ensure_ascii=False, sort_keys=True).encode()
    ).hexdigest()[:24]
    return {
        "receipt_id": f"rcpt-{digest}",
        "candidate_hash": candidate_hash,
        "tests_hash": tests_hash,
        "contract_id": contract_id,
        "result": result,
        "issuer": issuer,
        "utc": utc,
        "seq": seq,
    }


def write_receipt(dirpath, receipt):
    d = Path(dirpath)
    d.mkdir(parents=True, exist_ok=True)
    p = d / f"{receipt['seq']:04d}-{receipt['receipt_id']}.json"
    p.write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return p


def load_receipts(dirpath):
    d = Path(dirpath)
    if not d.exists():
        return []
    out = []
    for p in sorted(d.glob("*.json")):
        try:
            out.append(json.loads(p.read_text(encoding="utf-8")))
        except ValueError:
            continue
    return sorted(out, key=lambda r: (r.get("seq", 0), r.get("receipt_id", "")))


def listing(transport, receipts, *, receipt_id=None, required_issuer=None):
    """`list_receipts` 工具的返回值。"""
    if transport not in TRANSPORTS:
        raise ValueError(f"unknown transport {transport!r}")
    if transport == "report_only":
        return {"ok": False, "error": "receipt_listing_unavailable"}
    found = [r for r in receipts if receipt_id is None or r.get("receipt_id") == receipt_id]
    return {
        "ok": True,
        "receipts": found,
        "required_issuer": required_issuer if transport == "issuer_bound" else None,
    }


def acceptable(receipt, *, candidate_hash, tests_hash, contract_id, required_issuer=None):
    """返回 (是否可接受, 原因)。原因在不可接受时给出首个失配项。"""
    if receipt.get("result") != "pass":
        return False, "result_not_pass"
    if receipt.get("candidate_hash") != candidate_hash:
        return False, "candidate_version_mismatch"
    if receipt.get("tests_hash") != tests_hash:
        return False, "tests_version_mismatch"
    if receipt.get("contract_id") != contract_id:
        return False, "contract_mismatch"
    if required_issuer is not None and receipt.get("issuer") != required_issuer:
        return False, "issuer_not_permitted"
    return True, "acceptable"


def first_acceptable(receipts, **kw):
    for r in receipts:
        ok, _why = acceptable(r, **kw)
        if ok:
            return r
    return None


def contract_text(transport, required_issuer=None):
    """披露给模型的凭据条款。三个水平下交付物与验收门槛相同，只有这段不同。"""
    if transport not in TRANSPORTS:
        raise ValueError(f"unknown transport {transport!r}")
    common = (
        "A pass review requires a genuine successful test receipt bound to the delivered "
        "module.py and test_cases.json versions."
    )
    if transport == "report_only":
        return common + (
            " Receipts are returned to the session that ran the tests; there is no tool for "
            "retrieving a receipt issued by another session."
        )
    if transport == "shared_receipts":
        return common + (
            " Receipts are shared objects: list_receipts() returns every receipt for this "
            "delivery with its receipt_id, bound versions, result and issuer, and "
            "list_receipts(receipt_id) returns one. Any genuine receipt bound to the current "
            "versions is acceptable regardless of which session issued it."
        )
    issuer = required_issuer or "controller"
    return common + (
        f" Receipts are shared objects: list_receipts() returns every receipt with its "
        f"receipt_id, bound versions, result and issuer. Only receipts issued by {issuer} "
        f"are acceptable for release; a genuine receipt from any other issuer does not "
        f"satisfy the gate."
    )
