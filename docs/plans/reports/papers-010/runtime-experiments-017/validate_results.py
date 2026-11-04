"""Read-only provenance and instrumentation audit; does not rescore frozen results."""
from pathlib import Path
import json,hashlib
ROOT=Path(__file__).parent
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def typed_equal(a,b):
    if type(a) is not type(b):return False
    if isinstance(a,dict):return a.keys()==b.keys() and all(typed_equal(a[k],b[k]) for k in a)
    if isinstance(a,list):return len(a)==len(b) and all(typed_equal(x,y) for x,y in zip(a,b))
    return a==b
def main():
    lock=json.loads((ROOT/'design-lock.json').read_text());design=json.loads((ROOT/'design.json').read_text())['runs']
    frozen_ok={f:sha(ROOT/f)==d for f,d in lock['files'].items()}
    failures=[];manifest=[];request_count=0;worker_processes=0;calls_before_freeze=[];type_ambiguities=[]
    for row in design:
        ep=ROOT/'episodes'/row['id']
        if not (ep/'result.json').exists():failures.append({'id':row['id'],'error':'missing_result'});continue
        result=json.loads((ep/'result.json').read_text())
        if result.get('row')!=row:failures.append({'id':row['id'],'error':'row_changed'})
        seed=json.loads((ep/'seed-execution.json').read_text())
        if seed['status']!='failed' or seed['model_generated'] is not False or seed['candidate_check']['passed']:failures.append({'id':row['id'],'error':'seed_not_fixed_failure'})
        if result.get('status')=='harness_exception':failures.append({'id':row['id'],'error':'harness_exception'});continue
        expected_files=result['snapshot']['files'];state=ep/'stop-state/work'
        for name,info in expected_files.items():
            if not (state/name).exists() or sha(state/name)!=info['sha256']:failures.append({'id':row['id'],'error':'snapshot_hash_mismatch','file':name})
        for p in ep.glob('calls/*/*/attempt-*/request.json'):
            req=json.loads(p.read_text());request_count+=1
            if req['started_utc']<lock['frozen_utc']:calls_before_freeze.append(str(p))
            prompt=json.dumps({'conversation':req['conversation']},ensure_ascii=False)
            if hashlib.sha256(prompt.encode()).hexdigest()!=req['prompt_sha256']:failures.append({'id':row['id'],'error':'prompt_hash_mismatch','path':str(p)})
            for message in req['conversation']:
                if message['role'] in ['tool','runtime_state'] and ('private-final' in json.dumps(message) or 'terminal-evaluator' in json.dumps(message)):
                    failures.append({'id':row['id'],'error':'terminal_feedback_leak','path':str(p)})
        worker_pids=[]
        for p in ep.glob('workers/worker-*-result.json'):
            w=json.loads(p.read_text());worker_pids.append(w['pid']);worker_processes+=1
        if len(worker_pids)!=len(set(worker_pids)):failures.append({'id':row['id'],'error':'duplicate_worker_pid_within_episode'})
        for c in result['final_acceptance']['implementation'].get('checks',[]):
            if c['passed'] and not typed_equal(c['expected'],c['observed']):type_ambiguities.append({'id':row['id'],'check':c})
        # P1 writes only module.py; P2 artifact provenance remains role-scoped.
        from runtime import ROLES
        role_by_actor={w['worker_id']:w['role'] for w in result['worker_summaries']};role_by_actor.update({'executor-00':'executor','controller':'single' if row.get('identity')=='single' else None})
        for p in (ep/'events').glob('*.jsonl'):
            for line in p.read_text().splitlines():
                e=json.loads(line)
                if e['kind']=='file_write':
                    role=role_by_actor.get(e['actor']);name=e['value']['file'];version=ep/'versions'/e['value']['sha256']/name
                    if not role or name not in ROLES[role]:failures.append({'id':row['id'],'error':'role_write_escape','event':e})
                    if not version.exists() or sha(version)!=e['value']['sha256']:failures.append({'id':row['id'],'error':'version_not_preserved','event':e})
        for p in ep.rglob('*'):
            if p.is_file():manifest.append({'path':str(p.relative_to(ROOT)),'bytes':p.stat().st_size,'sha256':sha(p)})
    result={'frozen_sources_unchanged':all(frozen_ok.values()),'source_checks':frozen_ok,'planned_episodes':len(design),'request_hashes_checked':request_count,'real_worker_processes':worker_processes,'pre_freeze_main_requests':calls_before_freeze,'failures':failures,'numeric_or_boolean_type_ambiguities':type_ambiguities,'raw_files':len(manifest),'raw_bytes':sum(x['bytes'] for x in manifest),'scope':'Instrumentation/provenance checks, not independent human interpretation or general scientific validation.'}
    (ROOT/'analysis/validation.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    (ROOT/'raw-manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps({k:v for k,v in result.items() if k!='source_checks'},ensure_ascii=False))
    assert result['frozen_sources_unchanged'] and not calls_before_freeze and not failures and not type_ambiguities
if __name__=='__main__':main()
