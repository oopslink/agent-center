"""Supplementary read-only audit. Endpoints remain those of frozen analyze.py.

Adds full accounting, observable action indexes, and hand-checkable case excerpts;
does not change scores or label internal cognition.
"""
from pathlib import Path
from collections import Counter
import json,hashlib,statistics
ROOT=Path(__file__).parent
def load(p):return json.loads(p.read_text())
def dump(p,v):p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def events(ep):
    return sorted([json.loads(l) for p in (ep/'events').glob('*.jsonl') for l in p.read_text().splitlines()],key=lambda x:x['utc'])
def main():
    selected=set(load(ROOT/'analysis/case-selection.json')['ids'])
    excerpts=[];rows=[];prompts=[];ledger=[];errorcases=[]
    for folder in ['pilot','pilot-v2','pilot-v3','episodes-v1','episodes']:
        calls=[load(p) for p in (ROOT/folder).glob('*/calls/*/*/attempt-*/metadata.json')]
        results=[load(p) for p in (ROOT/folder).glob('*/result.json')]
        ledger.append({'folder':folder,'completed':len(results),'attempts':len(calls),
                       'reported_cost_usd':sum(x.get('cost_usd_reported',0) for x in calls),
                       'statuses':dict(Counter(x['status'] for x in results)),
                       'recorded_autonomous_delivery':sum(bool(x.get('autonomous_delivery')) for x in results),
                       'primary':folder=='episodes'})
    for ep in sorted((ROOT/'episodes').iterdir()):
        result=load(ep/'result.json');evs=events(ep);actions=[e for e in evs if e['kind']=='model_action']
        first=load(ep/'calls/controller/01/attempt-1/request.json')
        goal=first['conversation'][0]['content']
        prompts.append({'id':ep.name,'system':first['system'],'goal':goal['goal'],'contract':goal['user_contract'],
                        'source':str((ep/'calls/controller/01/attempt-1/request.json').relative_to(ROOT)),
                        'sha256':sha(ep/'calls/controller/01/attempt-1/request.json'),
                        'context_messages_at_start':len(first['conversation']),
                        'forbidden_normative_phrase_present':any(x in (first['system']+json.dumps(goal)).lower()
                            for x in ['must repair','must recover','remain responsible','complex workflows are harmful','identity confusion'])})
        errors=[e for e in evs if e['kind']=='tool_result' and (e['value'].get('error') or (e['value'].get('user_tests') or {}).get('error'))]
        codefails=[e for e in evs if e['actor'].startswith('worker-') and e['kind']=='code_check' and not e['value']['passed']]
        malformed=[e for e in actions if e['value']['action'] is None]
        if errors or codefails or malformed:
            errorcases.append({'id':ep.name,'status':result['status'],'autonomous_delivery':result['autonomous_delivery'],
                               'malformed':malformed,'errors':errors,'worker_failed_checks':codefails,
                               'all_actions':actions,'raw_path':str(ep)})
        rows.append({'id':ep.name,'controller_actions':[{'turn':e['value']['turn'],'op':(e['value']['action'] or {}).get('op','invalid')}
                    for e in actions if e['actor']=='controller'],'errors':errors,'worker_failed_checks':codefails,
                    'status':result['status'],'final_errors':result['final_acceptance']['errors']})
        if ep.name in selected:
            # Preserve actual notes and assignments, not a model-generated interpretation.
            excerpts.append({'id':ep.name,'row':result['row'],'status':result['status'],
                             'autonomous_delivery':result['autonomous_delivery'],
                             'final_acceptance':result['final_acceptance'],'controller_actions':
                             [e for e in actions if e['actor']=='controller'],
                             'worker_reports':result['worker_summaries'],
                             'errors':errors,'source_result_sha256':sha(ep/'result.json'),
                             'raw_path':str(ep)})
    assert not any(x['forbidden_normative_phrase_present'] for x in prompts)
    dump(ROOT/'analysis/all-run-accounting.json',ledger)
    dump(ROOT/'analysis/all-episode-actions.json',rows)
    dump(ROOT/'analysis/selected-case-events.json',excerpts)
    dump(ROOT/'analysis/all-error-cases.json',{'selection':'All observed format/tool/candidate errors, including successful recoveries; supplementary exhaustive error audit, not a changed endpoint.','cases':errorcases})
    dump(ROOT/'analysis/researcher-prompt-audit.json',{'scope':'Mechanical exact-phrase screening plus preserved researcher-authored initial inputs; not proof of absence of all semantic cues. Tool contracts necessarily state task success criteria. Dynamic model-written assignments are observations, not researcher interventions.','rows':prompts})
    print(json.dumps({'accounting':ledger,'selected_cases':len(excerpts),'primary_action_indexes':len(rows),'prompt_inputs':len(prompts)}))
if __name__=='__main__':main()
