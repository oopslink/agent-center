# -*- coding: utf-8 -*-
from runtime import *
from collections import Counter,defaultdict
import csv, io, statistics
def csvout(name,rows):
 with (P/name).open('w',newline='') as f:
  out=csv.DictWriter(f,fieldnames=list(rows[0]));out.writeheader();out.writerows(rows)
def render_action(raw):
 # Secondary descriptive measure only, never executes a malformed response.
 try:return next(v for i,ch in enumerate(raw) if ch=='{' for v in [json.JSONDecoder().raw_decode(raw[i:])[0]] if isinstance(v,dict) and v.get('action') in OPS)
 except (StopIteration,ValueError,TypeError):return {}
def replay(w):
 st=load(w/'state.json')
 try:c=git(w,'rev-parse','--verify','refs/heads/candidate')
 except subprocess.CalledProcessError:c=None
 assert c==st['candidate']
 if not c:return False
 anc=subprocess.run(['git','merge-base','--is-ancestor',st['base'],c],cwd=w).returncode
 assert anc in [0,1]
 if st['case']=='I':return anc==0
 rows=list(csv.reader(io.StringIO(git(w,'show',c+':result.csv'))))
 return anc==0 and rows==[['key','count'],['api','2'],['web','1']]
def main():
 rows=[];turns=[];prefix=[];requests=[];blind=[];order={}
 def annotate(tag,where,kind,has):
  r=load(where/'turn.json')
  h=r['text'];a=r['action'];v=render_action(h)
  ref=hashlib.sha256(('019/anonymous/'+tag).encode()).hexdigest()[:12]
  order[ref]=tag;item={'key':ref,'execution_action':a,'can_dispatch':has,'response':h}
  blind.append(item)
  turns.append({'key':ref,'locator':tag,'stage':kind,'action':a,'expressed_action':v.get('action',''),'precondition':has,'text':h,'event':r['event']})
  return r
 for row in DESIGN:
  id=row['id'];ep=P/'episodes'/id;s0=ep/'prefix'/'step-0';h=load(ep/'prefix-history.json')
  if not (s0/'turn.json').exists():
   prefix.append({**row,'action':'transport_failure','expressed_action':''})
   for a in ARMS:rows.append({**row,'arm':a,'reason':'prefix_transport_failure','rounds':0,'actions':'','dispatches':0,'candidate_pass':False,'accepted':False})
   continue
  t=annotate(id+'/0',s0,'initial',row['access']);assert h[:4]==load(ep/'initial.json');assert h[-1]['message']==t['event'];prefix.append({**row,'action':t['action'],'expressed_action':turns[-1]['expressed_action']})
  for a in ARMS:
   bp=ep/a;end=load(bp/'terminal.json');st=end['state'];passed=replay(bp/'work')
   assert passed==end['independent_check']['accepted_checks'] and (not st['accepted'] or passed)
   if not row['access']:assert not st['launches'] and not passed
   assert bool(st['returned_to'])==(a=='R')
   arms=load(bp/'intervention.json')
   step1=load(bp/'step-1/call/attempt-1/request.json')['conversation']
   assert step1==h+arms
   for k in range(1,len(end['actions'])+1):annotate(id+'/'+a+'/'+str(k),bp/f'step-{k}','branch',row['access'])
   rows.append({**row,'arm':a,'reason':end['reason'],'rounds':len(end['actions']),'actions':'/'.join(end['actions']),'dispatches':st['launches']-t['state_after']['launches'],'candidate_pass':passed,'accepted':st['accepted']})
 for p in sorted((P/'episodes').rglob('metadata.json')):
  v=load(p);q=load(p.parent/'request.json')
  requests.append({'locator':str(p.parent.relative_to(P)),'model':q['model_requested'],'reported':','.join(v['model_reported']),'ok':v['transport_ok'],'timeout':v['timeout'],'reported_usd':v['cost_usd_reported'],'utc':q['started_utc']})
 assert len(prefix)==16 and len(rows)==48
 csvout('prefix.csv',prefix);csvout('outcomes.csv',rows);csvout('requests.csv',requests)
 dump(P/'actions.json',turns);dump(P/'coding-key.json',order);dump(P/'coding-input.json',sorted(blind,key=lambda x:x['key']))
 summary={}
 for tier in [True,False]:
  groups={}
  for a in ARMS:
   sub=[x for x in rows if x['access']==tier and x['arm']==a]
   groups[a]={'n':len(sub),'accepted':sum(x['accepted'] for x in sub),'passed':sum(x['candidate_pass'] for x in sub),'reasons':dict(Counter(x['reason'] for x in sub)),'steps':sum(x['rounds'] for x in sub),'dispatches_after_prefix':sum(x['dispatches'] for x in sub)}
  summary['available' if tier else 'no_grant']={'prefix_actions':dict(Counter(x['action'] for x in prefix if x['access']==tier)),'arms':groups}
 summary.update(calls=len(requests),transport_failures=sum(not x['ok'] for x in requests),actions=len(turns),format_violations=sum(x['action']=='invalid' for x in turns),reported_usd=round(sum(x['reported_usd'] for x in requests),5))
 dump(P/'summary.json',summary);print(json.dumps(summary,ensure_ascii=False))
if __name__=='__main__':main()
