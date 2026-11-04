"""Prospective descriptive analysis; fixed before main outputs are collected."""
from pathlib import Path
from collections import Counter,defaultdict
import json,csv,statistics,hashlib
ROOT=Path(__file__).parent
def save(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
def events(ep):
    return sorted((json.loads(line) for p in (ep/'events').glob('*.jsonl') for line in p.read_text().splitlines()),key=lambda x:x['utc'])
def analyze():
    out=ROOT/'analysis';out.mkdir(exist_ok=True)
    design=json.loads((ROOT/'design.json').read_text())['runs'];rows=[];details={};invalid_blocks=set()
    for planned in design:
        ep=ROOT/'episodes'/planned['id'];p=ep/'result.json'
        if not p.exists():rows.append({**planned,'status':'missing','valid':False});invalid_blocks.add((planned['paper'],planned['task'],planned['model']));continue
        result=json.loads(p.read_text());evs=events(ep);calls=[json.loads(p.read_text()) for p in ep.glob('calls/*/*/attempt-*/metadata.json')]
        model_names=sorted(set(m for x in calls for m in x.get('model_reported',[])))
        valid=result['status'] not in ['transport_invalid','runtime_invalid','harness_exception'] and model_names==[planned['model']]
        if not valid:invalid_blocks.add((planned['paper'],planned['task'],planned['model']))
        actions=[x for x in evs if x['kind']=='model_action'];toolerrors=[x for x in evs if x['kind']=='tool_result' and x['value'].get('error')]
        workerfails=[x for x in evs if x['actor'].startswith('worker-') and x['kind']=='code_check' and not x['value']['passed']]
        failed_versions=set((x['actor'],x['value']['candidate_hash']) for x in workerfails)
        final=result.get('final_acceptance',{});check=final.get('implementation',{});hidden=check.get('checks',[])[3:]
        controllers=[x for x in actions if x['actor']=='controller']
        called=Counter((x['value']['action'] or {}).get('op','invalid') for x in controllers)
        token_totals={k:sum(x.get('usage',{}).get(k,0) for x in calls) for k in ['input_tokens','output_tokens','cache_creation_input_tokens','cache_read_input_tokens']}
        row={**planned,'status':result['status'],'valid':valid,'autonomous_delivery':bool(result.get('autonomous_delivery')),'independent_pass':bool(result.get('independent_pass_at_stop')),'implementation_pass':bool(check.get('passed')),'hidden_passed':sum(x['passed'] for x in hidden),'hidden_total':len(hidden),'premature_completion':bool(result.get('premature_completion')),'requested_external':result['status']=='requested_external','reported_blocked':result['status']=='reported_blocked','controller_turns':len(controllers),'worker_starts':result.get('worker_starts',0),'worker_decisions':len(actions)-len(controllers),'model_attempts':len(calls),'transport_retries':sum(x.get('attempt',1)>1 for x in calls),'format_errors':sum(x['value']['action'] is None for x in actions),'tool_error_count':len(toolerrors),'worker_failed_versions':len(failed_versions),'active_worker_at_stop':bool(result.get('active_worker_at_stop')),'late_artifact_change':bool(result.get('artifacts_changed_after_stop')),'cost_usd_reported':sum(x.get('cost_usd_reported',0) for x in calls),'model_duration_s_sum':sum(x.get('duration_s',0) for x in calls),'controller_spawn_actions':called['spawn'],'controller_wait_actions':called['wait'],'controller_inspect_actions':called['inspect'],'controller_check_actions':called['run_tests']+called['accept'],**token_totals}
        rows.append(row)
        details[planned['id']]={'row':row,'final_action':result.get('final_action'),'final_errors':final.get('errors',[]),'worker_failures':workerfails,'tool_errors':toolerrors,'first_controller_check_failure_budget':result.get('first_controller_check_failure_budget'),'raw_path':str(ep),'result_sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
    for r in rows:r['paired_block_valid']=(r['paper'],r['task'],r['model']) not in invalid_blocks
    keys=list(dict.fromkeys(k for r in rows for k in r))
    with (out/'episode-results.csv').open('w') as f:
        writer=csv.DictWriter(f,fieldnames=keys);writer.writeheader();writer.writerows(rows)
    save(out/'episode-details.json',details)
    groups=defaultdict(list)
    for r in rows:
        if r['paper']=='P1':condition=r['identity']+':'+r['ledger']
        else:condition=r['organization']+':'+r['binding']
        groups[(r['paper'],r['model'],condition)].append(r)
    summary=[]
    for (paper,model,condition),rs in sorted(groups.items()):
        valid=[r for r in rs if r.get('valid')]
        agg={'paper':paper,'model':model,'condition':condition,'planned':len(rs),'valid':len(valid),'autonomous_delivery':sum(r.get('autonomous_delivery',False) for r in valid),'independent_pass':sum(r.get('independent_pass',False) for r in valid),'implementation_pass':sum(r.get('implementation_pass',False) for r in valid),'premature_completion':sum(r.get('premature_completion',False) for r in valid),'reported_blocked':sum(r.get('reported_blocked',False) for r in valid),'requested_external':sum(r.get('requested_external',False) for r in valid)}
        for metric in ['controller_turns','worker_starts','model_attempts','cost_usd_reported','worker_failed_versions','tool_error_count']:
            vals=[r[metric] for r in valid];agg[metric]={'mean':statistics.mean(vals) if vals else None,'median':statistics.median(vals) if vals else None,'min':min(vals) if vals else None,'max':max(vals) if vals else None,'total':sum(vals)}
        summary.append(agg)
    save(out/'condition-summary.json',summary)
    pairs=[]
    factors=[('P1','identity','unified','separate','ledger'),('P1','ledger','on','off','identity'),('P2','organization','long','short','binding'),('P2','binding','joined','distributed','organization')]
    for paper,factor,a,b,other in factors:
        candidates=[r for r in rows if r['paper']==paper and r.get('identity')!='single' and r['paired_block_valid']]
        for left in candidates:
            if left[factor]!=a:continue
            matches=[r for r in candidates if r[factor]==b and r['task']==left['task'] and r['model']==left['model'] and r[other]==left[other]]
            assert len(matches)==1
            right=matches[0]
            pairs.append({'paper':paper,'factor':factor,'direction':a+' minus '+b,'task':left['task'],'model':left['model'],'other_condition':left[other],'left_id':left['id'],'right_id':right['id'],**{m+'_difference':float(left[m])-float(right[m]) for m in ['autonomous_delivery','independent_pass','controller_turns','worker_starts','model_attempts','cost_usd_reported']}})
    save(out/'paired-comparisons.json',pairs)
    selected=[r['id'] for r in rows if r.get('valid') and not r.get('autonomous_delivery')]
    # Every unsuccessful outcome plus the first successful ID per paper/model/condition.
    for key,rs in sorted(groups.items()):
        success=sorted(r['id'] for r in rs if r.get('valid') and r.get('autonomous_delivery'))
        if success:selected.append(success[0])
    save(out/'case-selection.json',{'rule':'All valid unsuccessful episodes; first successful ID per paper/model/condition; invalid runs separately retained. Selection was coded before main output collection.','ids':sorted(set(selected))})
    totals={'planned':len(rows),'completed':sum(r['status']!='missing' for r in rows),'valid':sum(r['valid'] for r in rows),'invalid_blocks':[list(x) for x in sorted(invalid_blocks)],'autonomous_delivery':sum(r.get('autonomous_delivery',False) for r in rows if r['valid']),'model_attempts':sum(r.get('model_attempts',0) for r in rows),'reported_cost_usd':sum(r.get('cost_usd_reported',0) for r in rows),'model_names':sorted(set(r['model'] for r in rows)),'no_significance_tests':'Three researcher-constructed task families per paper, one episode per cell/model. Pairs repeat tasks; no independent population CI or p value.'}
    save(out/'summary.json',totals);print(json.dumps(totals,ensure_ascii=False))
if __name__=='__main__':analyze()
