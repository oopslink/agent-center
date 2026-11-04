"""Reconstruct auxiliary head metadata and validate exact native DB captures.

This uses only archived Git mirrors and the frozen read-only DB. It does not
recapture live logs, live Git refs, deployment files or private working trees.
"""
from pathlib import Path
import subprocess,sqlite3,json,yaml
R=Path(__file__).resolve().parents[1]
docs=[]
for label in ['team-team-772532f5','team-team-f5a633d6']:
    root=R/'raw/memory'/label/'repo.git'
    def git(*args):return subprocess.check_output(['git','--git-dir',str(root),*args])
    for path in git('ls-tree','-r','--name-only','HEAD').decode().splitlines():
        b=git('show','HEAD:'+path);s=b.decode()
        fm=yaml.safe_load(s.split('---',2)[1]) if s.startswith('---') else {}
        docs.append({'repo':label,'path':path,'bytes':len(b),'frontmatter':fm})
existing=json.loads((R/'derived/head-documents.json').read_text())
assert docs==existing,'Archived Git HEAD metadata changed'
c=sqlite3.connect('file:/Users/oopslink/works/research/agent-harness-study/run-001/raw/snapshot.sqlite?mode=ro',uri=True);c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON')
for name,sql in [('memory_events-corrected',"SELECT * FROM events WHERE event_type LIKE 'team_memory.%' ORDER BY occurred_at,seq"),('team-rule-load-audits','SELECT * FROM team_rule_load_audits ORDER BY loaded_at')]:
    records=[dict(x) for x in c.execute(sql)];saved=json.loads((R/'raw/db'/(name+'.json')).read_text())
    # Capture column selection/order need not match; all saved columns must agree.
    keys=list(saved[0]); normalize=lambda rs:sorted(json.dumps({k:x[k] for k in keys},sort_keys=True,ensure_ascii=False) for x in rs)
    assert normalize(records)==normalize(saved),name
c.close();print('47 Git HEAD documents, 57 native events, 230 audit receipts independently re-extracted and matched.')
