# -*- coding: utf-8 -*-
from pathlib import Path
import sqlite3,json,re,datetime,collections
R=Path(__file__).resolve().parents[1]
c=sqlite3.connect('file:/Users/oopslink/works/research/agent-harness-study/run-001/raw/snapshot.sqlite?mode=ro',uri=True);c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON')
def dump(name,v):
 p=R/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
terms=['记忆','团队规则','团队规约','MEMORY.md','supervisor.md','team_memory','team memory','team rules','get_team_rule','origin-main-required','intermediate-candidate-nodes','integration-and-ship','evidence-before-verdict','production-like-testing','production-evidence-report','continuous-evolution','stop-repeated-evolution','hot-topic-radar-work-rules']
pat=re.compile('|'.join(map(re.escape,terms)),re.I)
allrows=c.execute('select id,conversation_id,sender_identity_id,content,created_at,context_refs from messages order by created_at,id')
rows=[]
for x in allrows:
 d=dict(x);matches=list(pat.finditer(d['content']))
 if matches:
  d['terms']=sorted({m.group(0) for m in matches});d['snippets']=[d['content'][max(0,m.start()-65):min(len(d['content']),m.end()+180)] for m in matches[:8]];rows.append(d)
dump('raw/db/memory-message-candidates.json',rows)
q=json.loads((R/'logs/queries.json').read_text());q.append({'name':'memory-message-candidates','sql':'select id,conversation_id,sender_identity_id,content,created_at,context_refs from messages order by created_at,id','client_filter_terms':terms,'matched':len(rows),'selection':'lexical candidate discovery, not evidence of actual reads or independent cases'})
# Resolve proposal evidence references and retain original malformed forms.
docs=json.loads((R/'derived/head-documents.json').read_text());refs=sorted({ref for d in docs for ref in d.get('frontmatter',{}).get('evidence_refs',[])})
resolved=[]
for ref in refs:
 kind,_,key=ref.partition(':');t,col={'message':('messages','id'),'task':('pm_tasks','id'),'plan':('pm_plans','id'),'issue':('pm_issues','id'),'conversation':('conversations','id')}.get(kind,(None,None))
 if t is None:resolved.append({'ref':ref,'status':'not_db_ref'});continue
 keys=[key]
 if kind in ['task','issue','plan'] and not key.startswith(kind+'-'):keys.append(kind+'-'+key)
 hit=[]
 for k in keys:
  try:hit=[dict(x) for x in c.execute('select * from '+t+' where '+col+'=?',(k,))]
  except sqlite3.OperationalError as e:hit=[]
  if hit:break
 resolved.append({'ref':ref,'keys_tried':keys,'status':'resolved' if hit else 'unresolved','records':hit})
dump('raw/db/proposal-source-records.json',resolved)
# Conversations supporting native proposal origins and later role handoffs.
conv_ids=set()
for v in resolved:
 for row in v.get('records',[]):
  if row.get('conversation_id'):conv_ids.add(row['conversation_id'])
  if v['ref'].startswith(('task:','issue:','plan:')):
   table={'task':'tasks','issue':'issues','plan':'plans'}[v['ref'].split(':')[0]]
   for rec in c.execute('select id from conversations where owner_ref=?',('pm://'+table+'/'+row['id'],)):conv_ids.add(rec['id'])
for cid in sorted(conv_ids):
 dump('raw/conversations/'+cid+'.json',[dict(x) for x in c.execute('select * from messages where conversation_id=? order by created_at,id',(cid,))])
q.append({'name':'proposal-source-records','refs':refs,'resolved':sum(x['status']=='resolved' for x in resolved),'conversation_ids':sorted(conv_ids),'note':'exact source IDs, with task-/issue-/plan- prefix completion explicitly recorded'})
dump('logs/queries.json',q);c.close()
print(json.dumps({'messages':len(rows),'terms':dict(collections.Counter(t for x in rows for t in x['terms'])),'refs':len(refs),'resolved':sum(x['status']=='resolved' for x in resolved),'conversations':len(conv_ids)},ensure_ascii=False))
