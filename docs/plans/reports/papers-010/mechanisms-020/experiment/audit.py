# -*- coding: utf-8 -*-
from study import *
from collections import Counter
import csv
def csvout(p,rows):
 with p.open('w',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(rows[0]));w.writeheader();w.writerows(rows)
rows=[];requests=[];turns=[]
for r in design:
 d=P/'episodes'/r['id'];v=read(d/'outcome.json');t=v['turns'];ob,_=init(r)
 assert ob==read(d/'initial.json')
 result=v['terminal_action'];target=r['target'];variant=r['variant']
 # independent scoring from the actual record exposed, not the condition name
 if r['paper']=='P3':
  current=[x for x in ob['records'] if x['candidate']==ob['contract']['candidate'] and x['project']==ob['session']['allowed_project'] and x.get('suite')==ob['contract']['required_suite']]
  expect='request' if not current else ('accept' if current[0]['check']=='passed' else 'reject')
 else:expect='close' if ob['phase']=='候选准备' and ob['facts']['remote_verified'] and ob['facts']['immutable_id'] else ('close' if ob['facts']['main_contains_candidate'] and ob['facts']['independent_review']=='已通过' else 'hold')
 assert expect==v['oracle'] and v['contract_match']==(expect==result)
 row={**r,'outcome':result,'expected_contract_action':expect,'matched':int(result==expect),'resolved_current_candidate':int(r['paper']=='P3' and result==expect and result in ['accept','reject']),'direct_first':t[0]['actual_action'] if t else 'transport','steps':len(t),'reason':v['reason']};rows.append(row)
 for j,step in enumerate(t,1):turns.append({'id':r['id'],**step})
 for f in sorted(d.glob('step-*/attempt-*/metadata.json')):
  v1=read(f);req=read(f.with_name('request.json'));requests.append({'id':r['id'],'locator':str(f.parent.relative_to(P)),'ok':v1['transport_ok'],'rc':v1['returncode'],'model':','.join(v1['model_reported']),'reported_price':v1['cost_usd_reported'],'utc':req['started_utc']})
  assert req['conversation'][0]['record']==ob and v1['model_reported']==[r['model']]
assert len(rows)==80
csvout(P/'results.csv',rows);csvout(P/'requests.csv',requests);save(P/'turns.json',turns)
summary={}
for k,dimensions in [('P3',['variant','view']),('P5',['variant','target'])]:
 sub=[x for x in rows if x['paper']==k];gr={}
 for tup in sorted({tuple(x[a] for a in dimensions) for x in sub}):
  g=[x for x in sub if tuple(x[a] for a in dimensions)==tup];gr['/'.join(tup)]={'n':len(g),'matched':sum(x['matched'] for x in g),'resolved':sum(x['resolved_current_candidate'] for x in g),'actions':dict(Counter(x['outcome'] for x in g)),'steps':sum(x['steps'] for x in g)}
 summary[k]=gr
summary['calls']=len(requests);summary['price']=round(sum(r['reported_price'] for r in requests),7);summary['transport_failures']=sum(not x['ok'] for x in requests);summary['format_errors']=sum(x['actual_action']=='invalid' for x in turns)
save(P/'summary.json',summary);print(json.dumps(summary))
