"""相对 episode 路径不得让检查静默全败。

`codecheck` 把 cases.json 的路径传给 `cwd=ep/work` 的子进程。若 episode 路径是
相对路径，子进程解析不到该文件，于是**每一次检查都返回 FileNotFoundError**——
产物看起来全不合格，而实际上模型的代码从未被执行过。rt017 的 H04 是同类事故：
仪器缺陷伪装成结果。这里把它钉住。
"""

import json
import os
import sys
from pathlib import Path

RUNNER = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(RUNNER))
import runtime as RT  # noqa: E402

TASK = "p2_records"
GOOD = """
def solve(data):
    totals = {}
    for item in data:
        qty = item["quantity"]
        if qty < 0:
            raise ValueError("negative quantity")
        name = item["name"].strip().casefold()
        if not name:
            continue
        totals[name] = totals.get(name, 0) + qty
    return [{"name": n, "quantity": totals[n]} for n in sorted(totals)]
"""


def build(ep):
    ep = Path(ep)
    for d in ["work", "versions", "events", "workers", "calls", "checks", "receipts"]:
        (ep / d).mkdir(parents=True, exist_ok=True)
    (ep / "row.json").write_text(json.dumps(
        {"id": "t", "paper": "P2", "organization": "short", "model": "unused",
         "task": TASK, "transport": "report_only"}), encoding="utf-8")
    (ep / "work" / "module.py").write_text(GOOD)
    RT._cfg_cache.clear()
    return ep


def test_absolute_episode_path_executes_the_check(tmp_path):
    ep = build(tmp_path / "ep-abs")
    out = RT.check(ep, "controller", TASK)
    assert out.get("error") != "FileNotFoundError", out
    assert "candidate_hash" in out


def test_relative_episode_path_also_executes_the_check(tmp_path, monkeypatch):
    ep = build(tmp_path / "ep-rel")
    monkeypatch.chdir(tmp_path.parent)
    relative = Path(os.path.relpath(ep, Path.cwd()))
    assert not relative.is_absolute()
    out = RT.check(relative, "controller", TASK)
    assert out.get("error") != "FileNotFoundError", out
    assert "candidate_hash" in out


def test_relative_and_absolute_paths_agree_on_the_verdict(tmp_path, monkeypatch):
    ep_abs = build(tmp_path / "ep-a")
    absolute = RT.check(ep_abs, "controller", TASK)
    ep_rel = build(tmp_path / "ep-b")
    monkeypatch.chdir(tmp_path.parent)
    relative = RT.check(Path(os.path.relpath(ep_rel, Path.cwd())), "controller", TASK)
    assert absolute["passed"] == relative["passed"]
