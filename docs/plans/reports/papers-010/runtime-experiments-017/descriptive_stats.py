"""Transparent additional summaries of frozen endpoints; no changed scoring."""
from pathlib import Path
from collections import defaultdict,Counter
import json,csv,statistics
ROOT=Path(__file__).parent
def main():
    with (ROOT/'analysis/episode-results.csv').open() as f:rows=list(csv.DictReader(f))
    assert len(rows)==54 and all(x['valid']=='True' for x in rows)
    for r in rows:
        for k in ['controller_turns','worker_decisions','worker_starts','model_attempts','format_errors','tool_error_count','worker_failed_versions']:
            r[k]=int(r[k])
        r['total_decisions']=r['controller_turns']+r['worker_decisions']
        r['cost_usd_reported']=float(r['cost_usd_reported'])
    groups=defaultdict(list)
    for r in rows:
        cond=r['identity']+':'+r['ledger'] if r['paper']=='P1' else r['organization']+':'+r['binding']
        groups[(r['paper'],r['model'],cond)].append(r)
    out=[]
    for (paper,model,cond),rs in sorted(groups.items()):
        data={'paper':paper,'model':model,'condition':cond,'n':len(rs),
              **{k:sum(x[k]=='True' for x in rs) for k in ['autonomous_delivery','independent_pass','implementation_pass','requested_external','premature_completion']}}
        for key in ['controller_turns','worker_decisions','total_decisions','worker_starts','model_attempts','cost_usd_reported']:
            a=[x[key] for x in rs];data[key]={'mean':statistics.mean(a),'median':statistics.median(a),'min':min(a),'max':max(a),'total':sum(a)}
        out.append(data)
    pairs=json.loads((ROOT/'analysis/paired-comparisons.json').read_text());by_id={r['id']:r for r in rows};pairgroups=defaultdict(list)
    for p in pairs:
        p['total_decisions_difference']=by_id[p['left_id']]['total_decisions']-by_id[p['right_id']]['total_decisions']
        p['worker_decisions_difference']=by_id[p['left_id']]['worker_decisions']-by_id[p['right_id']]['worker_decisions']
        pairgroups[(p['paper'],p['factor'],p['model'])].append(p)
    ps=[]
    for (paper,factor,model),rs in sorted(pairgroups.items()):
        data={'paper':paper,'factor':factor,'model':model,'n':len(rs),'direction':rs[0]['direction']}
        for key in ['autonomous_delivery_difference','controller_turns_difference','worker_decisions_difference','total_decisions_difference','model_attempts_difference','cost_usd_reported_difference']:
            a=[x[key] for x in rs];data[key]={'mean':statistics.mean(a),'median':statistics.median(a),'min':min(a),'max':max(a),'positive':sum(x>0 for x in a),'zero':sum(x==0 for x in a),'negative':sum(x<0 for x in a)}
        ps.append(data)
    extra=[]
    for paper in ['P1','P2']:
        rs=[r for r in rows if r['paper']==paper]
        extra.append({'paper':paper,'n':len(rs),'statuses':dict(Counter(r['status'] for r in rs)),
                      **{k:sum(r[k] for r in rs) for k in ['controller_turns','worker_decisions','total_decisions','worker_starts','model_attempts','format_errors','tool_error_count','worker_failed_versions','cost_usd_reported']}})
    result={'scope':'Algebraic summaries after collection of frozen per-episode endpoints; descriptive only, no new outcomes or exclusions. No population inference.','conditions':out,'paired_summaries':ps,'paper_totals':extra}
    (ROOT/'analysis/descriptive-summary.json').write_text(json.dumps(result,indent=2)+'\n')
    print(json.dumps(result))
if __name__=='__main__':main()
