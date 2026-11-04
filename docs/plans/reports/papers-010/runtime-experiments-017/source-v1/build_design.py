import json,random,itertools,hashlib
from pathlib import Path
from tasks import TASKS
ROOT=Path(__file__).parent
MODELS=['claude-sonnet-5-5','claude-haiku-5-5']
rows=[]
for task,t in TASKS.items():
    if t['split']!='main':continue
    for model in MODELS:
        m='sonnet' if 'sonnet' in model else 'haiku'
        if t['paper']=='P1':
            for identity,ledger in itertools.product(['unified','separate'],['off','on']):
                rows.append({'id':f'P1-{task}-{m}-{identity}-{ledger}','paper':'P1','task':task,'family':t['family'],'model':model,'identity':identity,'ledger':ledger})
            rows.append({'id':f'P1-{task}-{m}-single','paper':'P1','task':task,'family':t['family'],'model':model,'identity':'single','ledger':'off'})
        else:
            for org,binding in itertools.product(['short','long'],['distributed','joined']):
                rows.append({'id':f'P2-{task}-{m}-{org}-{binding}','paper':'P2','task':task,'family':t['family'],'model':model,'organization':org,'binding':binding,'ledger':'off','identity':'separate'})
random.Random(20261104).shuffle(rows)
(ROOT/'design.json').write_text(json.dumps({'seed':20261104,'models':MODELS,'runs':rows},ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'episodes':len(rows),'P1':sum(x['paper']=='P1' for x in rows),'P2':sum(x['paper']=='P2' for x in rows)}))
