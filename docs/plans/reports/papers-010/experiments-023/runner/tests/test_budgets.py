"""决策与启动预算必须可由 row 配置。

rt017 把 18/6 硬编码在 episode() 里。023 的试跑显示：在更难的任务上，
四角色条件会在闭合之前耗尽这两个预算，终态变成"预算耗尽"而不是协议结果。
要让终点落在协调成本而不是预算边界上，预算必须可调且被记录。
"""
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import runtime as RT


def test_defaults_match_rt017():
    assert RT.budgets({'paper': 'P1'}) == {'decisions': 12, 'worker_starts': 3}
    assert RT.budgets({'paper': 'P2'}) == {'decisions': 18, 'worker_starts': 6}


def test_row_overrides_are_honoured():
    got = RT.budgets({'paper': 'P2', 'decision_budget': 30, 'worker_start_budget': 10})
    assert got == {'decisions': 30, 'worker_starts': 10}


def test_partial_override_keeps_the_other_default():
    assert RT.budgets({'paper': 'P2', 'decision_budget': 24}) == {'decisions': 24, 'worker_starts': 6}


def test_non_positive_budget_is_rejected():
    for bad in ({'paper': 'P2', 'decision_budget': 0}, {'paper': 'P2', 'worker_start_budget': -1}):
        try:
            RT.budgets(bad)
        except ValueError:
            continue
        raise AssertionError(f'should have raised for {bad}')
