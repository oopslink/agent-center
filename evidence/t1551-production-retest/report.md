# T1551 production retest — REJECT

- Acceptance task: `task-eb532bbb` (T1551)
- Deployed candidate: `origin/main@9c16d4991bf46134f3018b79a8923b9438661a37`
- Fresh fixture plan: `plan-40ffec84`
- Fixture G0: `generation-85edec94`
- Source task: `task-19213287` (T1553)
- Owner: `agent:agent-d819c80f`
- Independent source runner: `agent:agent-ba6bc42a`
- Shared finding: `01M0XKDSXEYXWMMSZFDCABQ0XD`

## Result

REJECT. The deployed remediation still cannot supersede a genuinely blocked or
formally discarded terminal plan source, so it cannot create or dispatch the
continuation required to close I145.

## Fresh production observations

1. Production `get_plan` projected `owner_ref=pm://plans/plan-40ffec84`.
2. The independent source runner started the source and blocked it with
   `reason=T1551_FRESH_ISOLATED_BLOCK`, `reason_type=obstacle`.
3. Authority became `task=blocked`, `node=paused`; frontier became
   `human_decision`.
4. The Plan Owner received durable system Block Event message
   `01M0XKB09N5B2R1JYTHH7R719F` in the fixture plan conversation.
5. Owner atomic evolution (`base_version=5`, parent G0), with source
   `supersede` and an evidence-only supervisor-inline continuation linked by
   `follows_task_id`, failed:

   `orchestration: node in running/completed status cannot be removed`

6. After that failure, `get_plan` remained on G0/version 5 with only the
   blocked source: no new task, generation, continuation, or partial write.
7. The source was then formally discarded. Authority became
   `task=discarded`, `node=failed`; its stale frontier was cleared.
8. Owner terminal-source evolution with the same atomic supersede/continuation
   contract failed with the same orchestration error. Again there was no new
   generation or continuation dispatch.

An earlier intentionally invalid evolution containing a seq edge to the source
was also rejected (`task is not a node of this plan`) and rolled back cleanly;
it is treated only as negative rollback evidence, not as the acceptance result.

## Gate verdict

- Explicit owner projection: PASS
- Durable Owner-directed block event/wake: PASS
- Blocked-source atomic supersede: FAIL
- Terminal-source atomic supersede: FAIL
- Continuation creation and dispatch: NOT REACHED / FAIL
- No partial/orphan writes after rejected transaction: PASS
- Stale frontier cleanup after formal discard: PASS

Keep original T1544 `task-31b7cd50` blocked/held. Do not Resume, Bypass,
Discard, close I145, or release T1552. Plan Owner must create another Replace
remediation generation.
