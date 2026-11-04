#!/usr/bin/env python3
"""执行 023 正式批次。启动前校验 design-lock；可重入；到达金额上限即停。"""
import argparse,concurrent.futures as cf,hashlib,json,time,traceback
from pathlib import Path
from runtime import episode
from transport import now,save

ROOT=Path(__file__).resolve().parent
OUT=ROOT.parent/'episodes'
LOCK=ROOT.parent/'design-lock.json'

def verify_lock():
    lock=json.loads(LOCK.read_text())
    for f,digest in lock['files'].items():
        actual=hashlib.sha256((ROOT/f).read_bytes()).hexdigest()
        assert actual==digest,f'{f} 与 design-lock 不一致：期望 {digest} 实得 {actual}'
    return lock

def episode_cost(ep):
    return sum((json.load(open(p)).get('cost_usd_reported',0) or 0)
               for p in Path(ep).glob('calls/**/result.json'))

def main():
    ap=argparse.ArgumentParser()
    ap.add_argument('--workers',type=int,default=4)
    ap.add_argument('--cap-usd',type=float,default=200.0)
    args=ap.parse_args()
    lock=verify_lock()
    runs=json.loads((ROOT/'design023.json').read_text())['runs']
    OUT.mkdir(exist_ok=True)
    todo=[r for r in runs if not (OUT/r['id']/'result.json').exists()]
    spent=sum(episode_cost(OUT/r['id']) for r in runs if (OUT/r['id']/'result.json').exists())
    state={'started_utc':now(),'planned':len(runs),'already_done':len(runs)-len(todo),
           'todo':len(todo),'spent_usd':round(spent,5),'cap_usd':args.cap_usd,
           'lock_runs':lock['runs'],'complete':0,'failed':0,'stopped_on_cap':False}
    save(OUT.parent/'run-status.json',state)
    stop=False
    with cf.ThreadPoolExecutor(max_workers=args.workers) as pool:
        pending={}
        it=iter(todo)
        def submit_next():
            nonlocal stop
            if stop:return False
            try:r=next(it)
            except StopIteration:return False
            d=OUT/r['id']
            if d.exists():
                import shutil;shutil.rmtree(d)
            pending[pool.submit(episode,r,d)]=r
            return True
        for _ in range(args.workers):
            if not submit_next():break
        while pending:
            done,_=cf.wait(list(pending),return_when=cf.FIRST_COMPLETED)
            for fut in done:
                r=pending.pop(fut)
                try:
                    fut.result();state['complete']+=1
                except Exception:
                    state['failed']+=1
                    (OUT/r['id']).mkdir(parents=True,exist_ok=True)
                    (OUT/r['id']/'error.txt').write_text(traceback.format_exc())
                spent+=episode_cost(OUT/r['id'])
                state['spent_usd']=round(spent,5)
                state['last_utc']=now()
                save(OUT.parent/'run-status.json',state)
                if spent>=args.cap_usd:
                    stop=True;state['stopped_on_cap']=True
                submit_next()
    state['finished_utc']=now()
    state['missing']=sum(1 for r in runs if not (OUT/r['id']/'result.json').exists())
    save(OUT.parent/'run-status.json',state)
    print(json.dumps(state,ensure_ascii=False))

if __name__=='__main__':
    main()
