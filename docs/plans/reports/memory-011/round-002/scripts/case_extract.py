"""Purposeful case expansion from the frozen, read-only SQLite snapshot."""
from pathlib import Path
import json,sqlite3,datetime
R=Path(__file__).resolve().parents[1]
c=sqlite3.connect('file:/Users/oopslink/works/research/agent-harness-study/run-001/raw/snapshot.sqlite?mode=ro',uri=True)
c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON')
log=[]
def query(sql,args=()):
    rows=[dict(x) for x in c.execute(sql,args)];log.append({'sql':sql,'args':args,'rows':len(rows)});return rows
def dump(p,x):
    p=R/p;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
numbers=[1529,1530,1535,1536,1751,2222,2223,2224,1032]
tasks=query('SELECT * FROM pm_tasks WHERE org_number IN ('+','.join('?' for _ in numbers)+')',numbers)
tasks+=query('SELECT * FROM pm_tasks WHERE id=?',('task-d341b0d2',))
dump('raw/db/case-tasks.json',tasks)
actions=[];convs=set(['01KWHEW4D7H31KY53YF0867N69','01M0WH51Z022K0QN865QC90H93','01M2FMC6C6A3KN2KK2RZRY9V94'])
for t in tasks:
    actions+=query('SELECT * FROM pm_task_action_logs WHERE task_id=? ORDER BY occurred_at,id',(t['id'],))
    for x in query('SELECT * FROM conversations WHERE owner_ref=?',('pm://tasks/'+t['id'],)):convs.add(x['id'])
for ref in ['pm://issues/issue-b83b4192','pm://plans/plan-c93d8e6e','pm://plans/plan-523d5d71']:
    for x in query('SELECT * FROM conversations WHERE owner_ref=?',(ref,)):convs.add(x['id'])
dump('raw/db/case-task-actions.json',actions)
domain_events=[]
for tid in ['task-39dfd88f','task-2fdb1668','task-0179221b','task-0d32ed36']:
    domain_events+=query('SELECT * FROM events WHERE refs LIKE ? ORDER BY occurred_at,seq',('%'+tid+'%',))
dump('raw/db/case-domain-events.json',domain_events)
for cid in sorted(convs):dump('raw/conversations/'+cid+'.json',query('SELECT * FROM messages WHERE conversation_id=? ORDER BY created_at,id',(cid,)))
dump('logs/case-extraction.json',{'selection':'Theory-led chain expansion: strict main gate / intermediate handoff / analyst pending rules / legacy notes. Not a probability sample. Source messages discovered in the saved 585 lexical candidates and proposal evidence_refs.','task_numbers':numbers,'queries':log,'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()})
c.close();print(json.dumps({'tasks':len(tasks),'actions':len(actions),'conversations':len(convs)}))
