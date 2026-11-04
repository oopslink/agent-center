# 运行代码与研究构念：事前问题记录

仅静态读取代码；未执行模型、未看模型输出。初读代码仍在开发中，问题已发送主agent，最终闭合须绑定修订SHA。来源路径与SHA见JSON。

## C01 — major

位置：runtime.py:173-185, runtime.py:220-236。

active is an uncollected process handle, not a predicate that the process is running. Completed workers can be marked active_at_stop and cause premature completion despite finished valid artifacts.

修正要求：Poll actual process status at the stopping boundary; record completed-but-uncollected separately and do not make result collection an unstated success requirement.

## C02 — major

位置：runtime.py:52-88, runtime.py:90-116, runtime.py:220-230。

Public and user tests independently snapshot module.py, then a receipt binds only the latter hash. A concurrent writer can produce a receipt combining different code versions; test JSON reads and hash are likewise separated. Early-stop multi-file snapshot is not necessarily a single observed state.

修正要求：Execute checks against one atomic/frozen bundle or reject checks while writers are actually active using identical documented API rules. Treat early-stop race snapshot as uncertain auxiliary evidence, not exact state-at-stop.

## C03 — major_construct

位置：runtime.py:125-131。

P1 unified versus separate uses Supervisor/executors versus coordinator/workers, changing role vocabulary alongside identity affiliation.

修正要求：Hold Supervisor/executor terminology fixed and vary logical-identity relation only, or explicitly call the factor an identity-plus-label treatment.

## C04 — major_construct

位置：runtime.py:165-170。

A bootstrap initial implementation failure is not an executor failure handoff. If real model workers solve it once, the trajectory never tests post-executor-failure responsibility.

修正要求：Provide an explicitly researcher-seeded, traceable executor failure handoff with real subprocess artifact/check provenance, then model-driven recovery; alternatively narrow the RQ to delegated repair from an initial defect and keep natural executor-failure recovery descriptive. Do not fabricate a natural LLM failure.

## C05 — reporting_or_operationalization

位置：runtime.py:187-188。

Ledger freshly reads current artifact hashes each decision; history-only condition requires inspect to obtain changes. Treatment includes proactive state observation as well as persistence/format.

修正要求：Prespecify and report this intervention scope and token/action cost, or build both views from identical already-observed events if only persistence is intended.

## C06 — measurement

位置：runtime.py:171, runtime.py:208-213。

first_failure_budget ignores fixed initial failure and failed worker checks; it only captures the first failed controller run_tests/accept.

修正要求：Rename precisely and separately record initial opportunity plus worker-failure/rejection events and remaining budget.

