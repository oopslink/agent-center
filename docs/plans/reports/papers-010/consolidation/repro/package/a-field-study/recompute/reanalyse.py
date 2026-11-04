# -*- coding: utf-8 -*-
import csv,json,hashlib,statistics,os
from pathlib import Path
from collections import Counter,defaultdict
R=Path(os.environ.get('PAPER_ROOT','<ARCHIVE>/works/codes/<AUTHOR>/agent-center'))
A=Path(__file__).resolve().parent
B=R/'docs/plans/reports/papers-010'
ST=Path(os.environ.get('STUDY_ROOT','<ARCHIVE>/works/research/agent-harness-study/run-001'))
def save(p,x):p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
def read(p):return json.loads(p.read_text())
def csvwrite(p,rows):
 with p.open('w',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(rows[0]));w.writeheader();w.writerows(rows)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
results={};prov={}
p=B/'runtime-experiments-017/analysis/episode-results.csv';prov['P2']={str(p):sha(p)}
allrows=[r for r in csv.DictReader(p.open()) if r['paper']=='P2'];assert len(allrows)==24 and all(r['valid']=='True' and r['run_version']=='003-v2' for r in allrows)
pairs=[]
for key in sorted(set((r['task'],r['model'],r['binding']) for r in allrows)):
 s,l=[next(r for r in allrows if (r['task'],r['model'],r['binding'],r['organization'])==(*key,org)) for org in ['short','long']]
 row=dict(task=key[0],model=key[1],binding=key[2],short_id=s['id'],long_id=l['id'],both_delivered=s['autonomous_delivery']==l['autonomous_delivery']=='True')
 for col in ['controller_turns','worker_decisions','worker_starts','format_errors']:
  row[col]=int(l[col])-int(s[col])
 row['total']=row['controller_turns']+row['worker_decisions'];pairs.append(row)
csvwrite(A/'p2-pairs.csv',pairs)
def stat(rows):
 return {k:{'n':len(rows),'sum':sum(r[k] for r in rows),'median':statistics.median([r[k] for r in rows]),'min':min(r[k] for r in rows),'max':max(r[k] for r in rows),'positive':sum(r[k]>0 for r in rows)} for k in ['controller_turns','worker_decisions','total','worker_starts']}
results['P2']={'all':stat(pairs),'both_delivered':stat([r for r in pairs if r['both_delivered']])}
p=ST/'evolution-008/derived/generation-deltas.json';fixture=next(B.glob('p4-*/figure-data.json'))
prov['P4']={str(x):sha(x) for x in [p,fixture]}
skip={v['plan_id'] for v in read(fixture)['census']['fixture_plans']};assert len(skip)==6
ds=[x for x in read(p) if x['parent_found'] and x['plan_id'] not in skip];assert len(ds)==169 and len(set(x['plan_id'] for x in ds))==20 and all(x['same_plan_parent'] for x in ds)
rows=[]
for x in ds:
 status=x['state_changes'];keep=not bool(x['removed_task_ids']);top=any(x[z] for z in ['added_task_ids','removed_task_ids','added_edges','removed_edges']);assert top==x['topology_changed']
 rows.append(dict(id=x['id'],parent=x['parent'],plan=x['plan_id'],topology_changed=top,all_parent_nodes_retained=keep,shared_status_changes=len(status),became_completed=sum(a['after']=='completed' for a in status),left_completed=sum(a['before']=='completed' for a in status)))
csvwrite(A/'p4-transitions.csv',rows)
cross={};loo=[]
for t in [True,False]:
 sub=[r for r in rows if r['topology_changed']==t]
 cross[str(t)]={k:sum(bool(r[k]) for r in sub) for k in ['shared_status_changes','became_completed','left_completed','all_parent_nodes_retained']};cross[str(t)]['n']=len(sub)
for plan in sorted(set(r['plan'] for r in rows)):
 z=[r for r in rows if r['plan']!=plan];loo.append({'omitted_plan':plan,'n':len(z),'retained':sum(r['all_parent_nodes_retained'] for r in z),'frac':sum(r['all_parent_nodes_retained'] for r in z)/len(z)})
csvwrite(A/'p4-leave-one-plan-out.csv',loo);results['P4']={'cross':cross,'loo_retention_range':[min(r['frac'] for r in loo),max(r['frac'] for r in loo)]}
save(A/'reanalysis.json',results);save(A/'input-sources.json',prov);print(json.dumps(results))
