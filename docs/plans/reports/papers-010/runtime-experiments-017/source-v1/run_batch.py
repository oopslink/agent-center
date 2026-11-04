from pathlib import Path
from concurrent.futures import ThreadPoolExecutor,wait,FIRST_COMPLETED
import json,time,hashlib,argparse,traceback
from runtime import episode
from transport import save,now
ROOT=Path(__file__).parent
def cost_and_calls(folder):
    total=0;count=0
    for p in folder.glob('*/calls/*/*/attempt-*/metadata.json'):
        try:total+=json.loads(p.read_text()).get('cost_usd_reported',0);count+=1
        except (OSError,ValueError):pass
    return total,count
def main():
    ap=argparse.ArgumentParser();ap.add_argument('--pilot',action='store_true');ap.add_argument('--pilot-dir',default='pilot');args=ap.parse_args()
    if args.pilot:
        rows=[{'id':'dev-p1-unified','paper':'P1','task':'dev_abs','model':'claude-haiku-5-5','identity':'unified','ledger':'off'},
              {'id':'dev-p2-long','paper':'P2','task':'dev_reverse','model':'claude-haiku-5-5','identity':'separate','organization':'long','binding':'distributed','ledger':'off'}]
        folder=ROOT/args.pilot_dir
    else:
        lock=json.loads((ROOT/'design-lock.json').read_text())
        for f,d in lock['files'].items():assert hashlib.sha256((ROOT/f).read_bytes()).hexdigest()==d,f
        rows=json.loads((ROOT/'design.json').read_text())['runs'];folder=ROOT/'episodes'
    folder.mkdir(exist_ok=True);todo=[x for x in rows if not (folder/x['id']/'result.json').exists()]
    results=[json.loads(p.read_text()) for p in folder.glob('*/result.json')];cursor=0
    state={'utc':now(),'planned':len(rows),'complete':len(results),'inflight':0,'started':0}
    with ThreadPoolExecutor(max_workers=4) as pool:
        pending={}
        while cursor<len(todo) or pending:
            cost,calls=cost_and_calls(folder)
            while cursor<len(todo) and len(pending)<4 and cost<40:
                row=todo[cursor];cursor+=1;pending[pool.submit(episode,row,folder/row['id'])]=row
            if not pending:break
            done,_=wait(pending,timeout=5,return_when=FIRST_COMPLETED)
            for fut in done:
                row=pending.pop(fut)
                try:result=fut.result()
                except Exception:
                    result={'id':row['id'],'row':row,'status':'harness_exception','error':traceback.format_exc(),'finished_utc':now()};save(folder/row['id']/'result.json',result)
                results.append(result)
            state={'utc':now(),'stage':'pilot' if args.pilot else 'main','planned':len(rows),'complete':len(results),'inflight':len(pending),'started':cursor,'reported_cost_usd':cost,'model_attempts':calls,'autonomous_deliveries':sum(x.get('autonomous_delivery',False) for x in results),'invalid':sum(x['status'] in ['transport_invalid','runtime_invalid','harness_exception'] for x in results)}
            save(ROOT/(args.pilot_dir+'-progress.json' if args.pilot else 'progress.json'),state)
            if done:print(json.dumps(state),flush=True)
    save(ROOT/(args.pilot_dir+'-status.json' if args.pilot else 'run-status.json'),{**state,'finished_utc':now(),'missing':len(rows)-len(results)})

if __name__=='__main__':main()
