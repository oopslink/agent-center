#!/usr/bin/env python3
"""任务家族的无模型自洽性检查。在任何模型调用之前跑。

逐项断言：
1. reference 在每个向量上都能产出结果或确定的异常类型（不崩在别处）。
2. reference 与**手写期望**逐条一致 —— oracle 的第二来源。reference 错了，
   `expected()` 会静默定义错误的真值，这条检查是唯一的拦网。
3. seed 真的过不了公开向量（否则不存在恢复机会，整个 episode 没有失败空间）。
4. reference 通过全部向量（公开 + 隐藏）。
5. 向量数 ≥ 8、公开 3 个、隐藏 ≥ 5；输入互不相同。
6. 合法动作集合非空：即存在一条通过路径（reference 即是）。
7. spec 不泄漏待验证的原则（不出现 receipt / issuer / supervisor 等词）。

用法：selfcheck.py [--json]
"""

import argparse
import json
import sys

from tasks023 import TASKS023

LEAK_WORDS = ("receipt", "issuer", "supervisor", "executor", "must fix", "you are responsible")
PUBLIC = 3


def run_reference(src, value):
    scope = {}
    exec(src, scope)
    try:
        return {"output": scope["solve"](json.loads(json.dumps(value)))}
    except Exception as e:  # noqa: BLE001 — 异常类型本身是约定的结果
        return {"error": type(e).__name__}


def check_task(tid, task):
    problems = []
    inputs = task["inputs"]
    ref = task["reference"]

    if len(inputs) < 8:
        problems.append(f"向量不足 8 个：{len(inputs)}")
    if len(inputs) - PUBLIC < 5:
        problems.append(f"隐藏向量不足 5 个：{len(inputs) - PUBLIC}")
    seen = {json.dumps(x, sort_keys=True) for x in inputs}
    if len(seen) != len(inputs):
        problems.append("存在重复输入")

    low = task["spec"].lower()
    for w in LEAK_WORDS:
        if w in low:
            problems.append(f"spec 含待验证词汇：{w}")

    results = []
    for x in inputs:
        try:
            results.append(run_reference(ref, x))
        except Exception as e:  # reference 本身崩了
            problems.append(f"reference 执行失败：{type(e).__name__}: {e}")
            results.append(None)

    # 手写期望 vs reference
    hand = task.get("hand_checks") or []
    if len(hand) < 4:
        problems.append(f"手写期望不足 4 条：{len(hand)}")
    for value, want in hand:
        got = run_reference(ref, value)
        if got != want:
            problems.append(
                f"reference 与手写期望不一致 input={json.dumps(value, ensure_ascii=False)} "
                f"want={json.dumps(want, ensure_ascii=False)} got={json.dumps(got, ensure_ascii=False)}"
            )

    # seed 必须过不了公开向量
    seed_fail = False
    for x, want in zip(inputs[:PUBLIC], results[:PUBLIC]):
        if run_reference(task["seed"], x) != want:
            seed_fail = True
            break
    if not seed_fail:
        problems.append("seed 通过了全部公开向量：该 episode 没有失败空间")

    # reference 必须过全部向量（定义上成立，这里防 reference 不确定性）
    for x, want in zip(inputs, results):
        if run_reference(ref, x) != want:
            problems.append(f"reference 不确定：input={json.dumps(x, ensure_ascii=False)}")

    errs = sum(1 for r in results if r and "error" in r)
    return {
        "task": tid,
        "split": task["split"],
        "family": task["family"],
        "vectors": len(inputs),
        "public": PUBLIC,
        "hidden": len(inputs) - PUBLIC,
        "error_vectors": errs,
        "hand_checks": len(hand),
        "problems": problems,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()
    report = [check_task(t, v) for t, v in TASKS023.items()]
    bad = [r for r in report if r["problems"]]
    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=1))
    else:
        for r in report:
            mark = "FAIL" if r["problems"] else "ok  "
            print(f"{mark} {r['task']:<12} {r['split']:<12} 向量 {r['vectors']:>2} "
                  f"(隐藏 {r['hidden']}, 异常向量 {r['error_vectors']}) 手写期望 {r['hand_checks']}")
            for p in r["problems"]:
                print(f"       - {p}")
        print(f"\n{len(report) - len(bad)}/{len(report)} 个任务家族自洽")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
