# -*- coding: utf-8 -*-
from pathlib import Path
import shutil,json,datetime
R=Path(__file__).resolve().parents[1]
repo=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
for d in ['before','raw','derived','scripts','logs','after','sources']:(R/d).mkdir(parents=True,exist_ok=True)
for rel in ['docs/plans/reports/papers-010/p1-logical-agent-identity/paper.md','docs/plans/reports/papers-010/p1-logical-agent-identity/controlled-experiments.md','docs/plans/reports/papers-010/p1-logical-agent-identity/mechanism-analysis.md','docs/plans/reports/papers-010/p1-logical-agent-identity/evidence-details.md','docs/plans/reports/agent-harness-papers-010.md','docs/plans/reports/papers-010/p1-logical-agent-identity/body-count.json']:
 p=R/'before'/rel;p.parent.mkdir(parents=True,exist_ok=True)
 if not p.exists():shutil.copy2(repo/rel,p)
(R/'author-feedback.json').write_text(json.dumps({'captured_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source':'Current author message in Codex; retrospective operator account, not native historical DB row','verbatim':'# Supervisor–Executor 架构中的身份抽象与责任归属 这篇结论不对，Supervisor–Executor 架构 导致agent早期经常进入confuse状态，认为 executor 不是他自己，executor出错后不会自动处理，导致流程卡住。经过多次人工提醒后，才逐步进入正轨，但是偶尔还是会犯错。','scope':'Report the account; seek dated corroboration; do not convert 经常/偶尔 into rates or claim a training cause.'},ensure_ascii=False,indent=2)+'\n')
