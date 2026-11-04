# -*- coding: utf-8 -*-
from score import *
from conditioned import Q,ROWS
def main():
 rows=[];turns=[];req=[]
 for r in ROWS:
  ep=Q/'episodes'/r['id'];h=load(ep/'prefix-history.json')
  for a in ARMS:
   bp=ep/a;end=load(bp/'terminal.json');st=end['state'];pass_=replay(bp/'work')
   assert st['candidate']==end['independent_check']['candidate'] and end['independent_check']['accepted_checks']==pass_
   assert bool(st['returned_to'])==(a=='R') and (not st['accepted'] or pass_)
   assert load(bp/'step-1/call/attempt-1/request.json')['conversation']==h+load(bp/'intervention.json')
   for j in range(1,len(end['actions'])+1):
    t=load(bp/f'step-{j}'/'turn.json')
    turns.append({'key':hashlib.sha256(('019/add/'+r['id']+'/'+a+'/'+str(j)).encode()).hexdigest()[:12],'locator':r['id']+'/'+a+'/'+str(j),'stage':'conditioned','action':t['action'],'expressed_action':render_action(t['text']).get('action',''),'precondition':True,'text':t['text'],'event':t['event']})
   rows.append({**r,'arm':a,'reason':end['reason'],'rounds':len(end['actions']),'actions':'/'.join(end['actions']),'first':end['actions'][0] if end['actions'] else '','ever_dispatched':st['launches']>0,'candidate_pass':pass_,'accepted':st['accepted']})
 for p in sorted((Q/'episodes').rglob('metadata.json')):
  m=load(p);req.append({'locator':str(p.parent.relative_to(Q)),'ok':m['transport_ok'],'reported':','.join(m['model_reported']),'reported_usd':m['cost_usd_reported']})
 csvout('conditioned/outcomes.csv',rows);csvout('conditioned/requests.csv',req);dump(Q/'actions.json',turns)
 summary={a:{'n':len(ROWS),'first':dict(Counter(x['first'] for x in rows if x['arm']==a)),'ever_dispatched':sum(x['ever_dispatched'] for x in rows if x['arm']==a),'accepted':sum(x['accepted'] for x in rows if x['arm']==a),'passed':sum(x['candidate_pass'] for x in rows if x['arm']==a),'reasons':dict(Counter(x['reason'] for x in rows if x['arm']==a)),'steps':sum(x['rounds'] for x in rows if x['arm']==a)} for a in ARMS}
 summary.update(calls=len(req),transport_failures=sum(not x['ok'] for x in req),format_violations=sum(x['action']=='invalid' for x in turns),reported_usd=round(sum(x['reported_usd'] for x in req),5))
 dump(Q/'summary.json',summary);print(summary)
if __name__=='__main__':main()
