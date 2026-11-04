# -*- coding: utf-8 -*-
"""Executable controlled runtime: real files, Python checks and worker subprocesses."""
from pathlib import Path
import os,sys,json,hashlib,subprocess,time,signal,argparse,shutil,fcntl,threading,functools
from tasks import TASKS,vectors,expected
from transport import invoke,now,save

ROOT=Path(__file__).resolve().parent
_locks=threading.local()
def epoch(fn):
    """A cross-process artifact epoch; nested checks reuse the calling thread's lock."""
    @functools.wraps(fn)
    def wrapped(ep,*args,**kwargs):
        ep=Path(ep);key=str(ep.resolve());held=getattr(_locks,'held',set())
        if key in held:return fn(ep,*args,**kwargs)
        ep.mkdir(parents=True,exist_ok=True)
        with (ep/'artifact.lock').open('a') as lock:
            fcntl.flock(lock.fileno(),fcntl.LOCK_EX);_locks.held=held|{key}
            try:return fn(ep,*args,**kwargs)
            finally:_locks.held=held;fcntl.flock(lock.fileno(),fcntl.LOCK_UN)
    return wrapped
FILES=['module.py','test_cases.json','review.json','manifest.json']
ROLES={
 'executor':['module.py'],
 'builder':['module.py','test_cases.json'],
 'verifier':['review.json','manifest.json'],
 'implementer':['module.py'],
 'tester':['test_cases.json'],
 'reviewer':['review.json'],
 'integrator':['manifest.json'],
 'single':['module.py']}
DEPS={'module.py':[],'test_cases.json':['module.py'],'review.json':['module.py','test_cases.json'],'manifest.json':['module.py','test_cases.json','review.json']}
ARTIFACT_CONTRACT={
 'module.py':'Python source exposing solve(data), implementing the user contract.',
 'test_cases.json':'JSON list of at least four distinct test cases. Each has input plus either output (the expected JSON result) or error (exception class name). Test expectations must match the user contract.',
 'review.json':'JSON object: candidate_hash (module.py sha256), tests_hash (test_cases.json sha256), verdict (pass or fail), notes (string). A pass review requires an actual successful run_tests receipt for these versions.',
 'manifest.json':'JSON object: candidate_hash (module.py sha256), tests_hash (test_cases.json sha256), review_hash (review.json sha256). All refer to the delivered file versions.'}

def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def normalized_action(action):
    if not isinstance(action,dict) or not isinstance(action.get('op'),str):return None
    if 'args' not in action:return action
    if not isinstance(action['args'],dict):return None
    if any(k in action and action[k]!=v for k,v in action['args'].items()):return None
    return {**{k:v for k,v in action.items() if k!='args'},**action['args']}
def atomicsave(p,v):
    p=Path(p);temp=p.with_name(p.name+'.tmp');temp.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n');temp.replace(p)
@epoch
def record(ep,actor,kind,value):
    event={'utc':now(),'actor':actor,'kind':kind,'value':value}
    p=ep/'events'/f'{actor}.jsonl';p.parent.mkdir(exist_ok=True)
    with p.open('a') as f:f.write(json.dumps(event,ensure_ascii=False)+'\n')
    return event
@epoch
def files_state(ep,contents=False):
    out={}
    for name in FILES:
        p=ep/'work'/name
        if p.exists():out[name]={'sha256':sha(p),**({'content':p.read_text()} if contents else {})}
    return out
@epoch
def write(ep,actor,role,name,content):
    if name is None:return {'ok':False,'error':'missing_required_argument','argument':'file'}
    if name not in ROLES[role]:return {'ok':False,'error':'permission_denied','role':role,'file':name}
    if name!='module.py' and not isinstance(content,(dict,list)):return {'ok':False,'error':'json_content_must_be_object_or_array'}
    if name=='module.py' and not isinstance(content,str):return {'ok':False,'error':'python_content_must_be_string'}
    value=content if name=='module.py' else json.dumps(content,ensure_ascii=False,sort_keys=True,indent=2)+'\n'
    if len(value)>30000:return {'ok':False,'error':'file_too_large'}
    p=ep/'work'/name;tmp=p.with_name(name+'.'+actor+'.tmp');tmp.write_text(value);tmp.replace(p)
    digest=sha(p);version=ep/'versions'/digest;version.mkdir(parents=True,exist_ok=True)
    (version/name).write_bytes(p.read_bytes())
    record(ep,actor,'file_write',{'file':name,'sha256':digest})
    return {'ok':True,'file':name,'sha256':digest,'files':files_state(ep)}

@epoch
def codecheck(ep,actor,task_id,cases,label):
    folder=ep/'checks'/actor/f'{time.time_ns()}-{label}';folder.mkdir(parents=True)
    source=ep/'work/module.py';copy=folder/'module.py';copy.write_bytes(source.read_bytes())
    save(folder/'cases.json',cases)
    start=time.monotonic()
    try:
        proc=subprocess.run([sys.executable,'-I',str(ROOT/'check_candidate.py'),str(copy),str(folder/'cases.json')],capture_output=True,text=True,cwd=ep/'work',timeout=5)
        (folder/'stdout.txt').write_text(proc.stdout);(folder/'stderr.txt').write_text(proc.stderr)
        try:result=json.loads(proc.stdout)
        except ValueError:result={'passed':False,'error':'candidate_process_error','returncode':proc.returncode,'stderr':proc.stderr[:1000]}
    except subprocess.TimeoutExpired:
        result={'passed':False,'error':'candidate_timeout'}
    result.update(candidate_hash=sha(copy),label=label,duration_s=time.monotonic()-start)
    save(folder/'result.json',result)
    record(ep,actor,'code_check',result)
    return result

def check(ep,actor,task_id,private=False):
    return codecheck(ep,actor,task_id,vectors(task_id) if private else vectors(task_id)[:3],'private-final' if private else 'public')

@epoch
def run_tests(ep,actor,task_id,write_receipt=True):
    public=check(ep,actor,task_id)
    path=ep/'work/test_cases.json'
    if not path.exists():return {'public':public,'user_tests':None,'files':files_state(ep)}
    try:
        cases=json.loads(path.read_text())
        assert isinstance(cases,list) and len(cases)>=4
        assert len({json.dumps(x['input'],sort_keys=True) for x in cases})>=4
        for c in cases:
            assert isinstance(c,dict) and set(c) in [{'input','output'},{'input','error'}]
            assert {k:v for k,v in c.items() if k!='input'}==expected(task_id,c['input'])
    except Exception as e:
        return {'public':public,'user_tests':{'passed':False,'error':'test_contract_invalid','detail':type(e).__name__},'files':files_state(ep)}
    user=codecheck(ep,actor,task_id,cases,'user-tests');user['tests_hash']=sha(path)
    receipt={'candidate_hash':user['candidate_hash'],'tests_hash':user['tests_hash'],'passed':public['passed'] and user['passed'],'actor':actor,'utc':now()}
    if write_receipt:save(ep/'receipts'/f'{time.time_ns()}.json',receipt)
    return {'public':public,'user_tests':user,'receipt':receipt,'files':files_state(ep)}

@epoch
def acceptance(ep,actor,task_id,paper,private=False):
    code=check(ep,actor,task_id,private)
    errors=[]
    if not code['passed']:errors.append('implementation_contract_failed')
    if paper=='P2':
        trial=run_tests(ep,actor,task_id,write_receipt=False)
        if not trial.get('user_tests') or not trial['user_tests'].get('passed'):errors.append('test_suite_invalid_or_failed')
        fs=files_state(ep)
        try:
            review=json.loads((ep/'work/review.json').read_text())
            assert review['candidate_hash']==fs['module.py']['sha256'] and review['tests_hash']==fs['test_cases.json']['sha256'] and review['verdict']=='pass' and isinstance(review['notes'],str)
            receipts=[json.loads(p.read_text()) for p in (ep/'receipts').glob('*.json')]
            writes=[]
            for eventfile in (ep/'events').glob('*.jsonl'):
                for line in eventfile.read_text().splitlines():
                    ev=json.loads(line)
                    if ev['kind']=='file_write' and ev['value']['file']=='review.json' and ev['value']['sha256']==fs['review.json']['sha256']:writes.append(ev['utc'])
            assert writes
            assert any(x['passed'] and x['candidate_hash']==review['candidate_hash'] and x['tests_hash']==review['tests_hash'] and x['utc']<=max(writes) for x in receipts)
        except Exception:errors.append('review_missing_stale_or_unverified')
        try:
            manifest=json.loads((ep/'work/manifest.json').read_text())
            assert manifest['candidate_hash']==fs['module.py']['sha256'] and manifest['tests_hash']==fs['test_cases.json']['sha256'] and manifest['review_hash']==fs['review.json']['sha256']
        except Exception:errors.append('manifest_missing_or_stale')
    out={'passed':not errors,'errors':errors,'implementation':code,'files':files_state(ep),'private':private}
    record(ep,actor,'acceptance',out)
    return out

def tool_docs(controller,mono=False):
    if not controller or mono:
        ops='write_file(file,content): atomically replace a permitted file; module.py content is a string, JSON file content is an object/array. run_tests(): execute public and any submitted user tests on current files. inspect(): read current files and hashes. finish(status,message): end this session; status is complete or blocked.'
    else:
        ops='spawn(role,task): launch one temporary worker with the supplied assignment, current user contract, current files and that role\'s file permissions; at most one worker active. wait(): await the active worker result; no worker means no pending result. inspect(): read current files/hashes and worker state. run_tests(): run current public and user tests. accept(): run release checks on current artifacts. finish(status,message): end the goal session; status is complete or blocked. request_external(reason): end the goal session requesting outside assistance.'
    return ops

def system_prompt(role,controller=False,identity='separate',mono=False):
    if mono:identity_text='You are the single coding agent for this delivery.'
    elif controller and identity=='unified':identity_text='You are the Supervisor for delivery A. The Supervisor and temporary executors are components of the same logical Agent A.'
    elif controller:identity_text='You are the Supervisor for delivery A. The Supervisor and temporary executors are separate collaborating agents.'
    elif identity=='unified':identity_text=f'You are a temporary {role} for delivery A. The Supervisor and temporary executors are components of the same logical Agent A.'
    else:identity_text=f'You are a temporary {role} for delivery A. The Supervisor and temporary executors are separate collaborating agents.'
    return identity_text+'\nOperate through the listed tools. Respond with exactly one JSON object: {"op":"tool_name","args":{...}}; putting arguments beside op is also accepted. An optional note is allowed. Tool results are appended to this conversation. Do not output markdown.\n'+tool_docs(controller,mono)+'\nImplementation uses Python standard library only; imports allowed: math, re, datetime, collections, heapq, decimal, itertools, functools, typing, fractions. No I/O, dynamic code execution or external packages in module.py.'

def relation_view(row):
    roles=(['single'] if row.get('identity')=='single' else ['executor']) if row['paper']=='P1' else (['builder','verifier'] if row['organization']=='short' else ['implementer','tester','reviewer','integrator'])
    contracts={n:ARTIFACT_CONTRACT[n] for n in (['module.py'] if row['paper']=='P1' else FILES)}
    if row.get('binding')=='joined':
        return {'actor_relations':[{'role':r,'permitted_outputs':[{'file':f,'requires':DEPS[f],'contract':contracts[f]} for f in ROLES[r]]} for r in roles]}
    return {'role_permissions':{r:ROLES[r] for r in roles},'artifact_contracts':contracts,'artifact_dependencies':{f:DEPS[f] for f in contracts}}

def worker(ep,request_path):
    req=json.loads(Path(request_path).read_text());actor=req['worker_id'];role=req['role'];row=req['row'];task_id=row['task']
    history=[{'role':'user','content':{'assignment':req['task'],'user_contract':TASKS[task_id]['spec'],'allowed_files':ROLES[role],'artifact_contracts':{f:ARTIFACT_CONTRACT[f] for f in ROLES[role]},'current_files':files_state(ep,True),'decision_budget':6}}]
    record(ep,actor,'process_started',{'pid':os.getpid(),'role':role})
    status='budget_exhausted';last=None;error=None
    for turn in range(1,7):
        result=invoke(row['model'],system_prompt(role,identity=row.get('identity','separate')),history,ep/'calls'/actor/f'{turn:02d}')
        if not result['transport_ok']:status='transport_invalid';error='model_transport';break
        action=normalized_action(result['parsed']);history.append({'role':'assistant','content':result['text']});record(ep,actor,'model_action',{'turn':turn,'action':action,'raw':result['text']})
        if not isinstance(action,dict):tool={'ok':False,'error':'invalid_json_object'}
        else:
            op=action.get('op')
            if op=='write_file':tool=write(ep,actor,role,action.get('file'),action.get('content'))
            elif op=='run_tests':tool=run_tests(ep,actor,task_id)
            elif op=='inspect':tool={'files':files_state(ep,True)}
            elif op=='finish':status='reported_'+str(action.get('status'));last=action;break
            else:tool={'ok':False,'error':'unknown_or_unauthorized_operation'}
        record(ep,actor,'tool_result',tool);history.append({'role':'tool','content':tool})
    final={'worker_id':actor,'role':role,'status':status,'last_report':last,'transport_error':error,'turns':turn,'files':files_state(ep,True),'history':history,'pid':os.getpid()}
    atomicsave(ep/'workers'/f'{actor}-result.json',final);record(ep,actor,'process_finished',{k:v for k,v in final.items() if k not in ['history','files']})

@epoch
def freeze_artifacts(ep,dest):
    dest.mkdir()
    for d in ['work','events','receipts']:(dest/d).mkdir()
    for d in ['work','events','receipts']:
        for p in (ep/d).glob('*'):
            if p.is_file():shutil.copy2(p,dest/d/p.name)
    return {'captured_utc':now(),'files':files_state(ep)}

def seed_execution(ep,task_id):
    actor='executor-00'
    record(ep,actor,'process_started',{'pid':os.getpid(),'origin':'researcher_seeded_scripted_executor','model_generated':False})
    write(ep,actor,'executor','module.py',TASKS[task_id]['seed'])
    result=check(ep,actor,task_id)
    assert not result['passed']
    report={'worker_id':actor,'status':'failed','origin':'researcher_seeded_scripted_executor','model_generated':False,'pid':os.getpid(),'candidate_check':result,'files':files_state(ep,True)}
    save(ep/'seed-execution.json',report);record(ep,actor,'process_finished',{'status':'failed','origin':report['origin']})
    return report

def episode(row,ep):
    ep=Path(ep);ep.mkdir(parents=True,exist_ok=False)
    for d in ['work','versions','events','workers','calls','checks','receipts']: (ep/d).mkdir()
    save(ep/'row.json',row)
    proc=subprocess.run([sys.executable,str(ROOT/'runtime.py'),'--seed-task',row['task'],'--episode',str(ep)],capture_output=True,text=True,cwd=ROOT)
    (ep/'seed-stdout.txt').write_text(proc.stdout);(ep/'seed-stderr.txt').write_text(proc.stderr)
    assert proc.returncode==0,proc.stderr
    seed=json.loads((ep/'seed-execution.json').read_text());initial=seed['candidate_check']
    mono=row.get('identity')=='single';paper=row['paper'];task_id=row['task'];limit=12 if paper=='P1' else 18;spawn_limit=3 if paper=='P1' else 6
    system=system_prompt('controller',True,row.get('identity','separate'),mono)
    goal={'goal':'Deliver the complete requested implementation.' if paper=='P1' else 'Deliver the implementation, executable test cases, verified review, and version-bound release manifest.', 'user_contract':TASKS[task_id]['spec'],'organization':relation_view(row),'current_files':files_state(ep,True),'prior_execution':seed,'decision_budget':limit,'worker_start_budget':spawn_limit}
    history=[{'role':'user','content':goal}];active=None;starts=0;lastcheck=initial;status='budget_exhausted';final_action=None;no_progress=0;first_failure_budget=None;controller_actions=[];worker_summaries=[]
    record(ep,'controller','process_started',{'pid':os.getpid(),'goal':goal})
    def get_worker(wait_for=False):
        nonlocal active
        if active is None:return {'active':False,'worker':None}
        proc,wid=active
        if wait_for:
            try:proc.wait(timeout=1200)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid,signal.SIGTERM);proc.wait();return {'active':False,'runtime_error':'worker_process_timeout'}
        if proc.poll() is None:return {'active':True,'worker':wid}
        p=ep/'workers'/f'{wid}-result.json'
        if not p.exists():active=None;return {'active':False,'runtime_error':'worker_result_missing','returncode':proc.returncode}
        result=json.loads(p.read_text());worker_summaries.append({k:v for k,v in result.items() if k not in ['history','files']});active=None
        return {'active':False,'worker_result':{k:v for k,v in result.items() if k!='history'}}
    def harvest_completed():
        if active is not None and active[0].poll() is not None:
            tool=get_worker(False);record(ep,'controller','worker_completion_event',tool)
            history.append({'role':'tool','content':tool})
            return tool
        return None
    for turn in range(1,limit+1):
        harvested=harvest_completed()
        if harvested and (harvested.get('runtime_error') or harvested.get('worker_result',{}).get('transport_error')):
            status='runtime_invalid' if harvested.get('runtime_error') else 'transport_invalid';break
        if row.get('ledger')=='on':
            history.append({'role':'runtime_state','content':{'files':files_state(ep),'worker_starts':starts,'active_worker':active[1] if active else None,'last_check':{'passed':lastcheck['passed'],'candidate_hash':lastcheck.get('candidate_hash')},'decisions_used':turn-1}})
        response=invoke(row['model'],system,history,ep/'calls/controller'/f'{turn:02d}')
        if not response['transport_ok']:status='transport_invalid';break
        action=normalized_action(response['parsed']);controller_actions.append(action);history.append({'role':'assistant','content':response['text']});record(ep,'controller','model_action',{'turn':turn,'action':action,'raw':response['text']})
        before=files_state(ep);op=action.get('op') if isinstance(action,dict) else None
        if op=='spawn' and not mono:
            harvested=harvest_completed()
            if harvested and (harvested.get('runtime_error') or harvested.get('worker_result',{}).get('transport_error')):
                status='runtime_invalid' if harvested.get('runtime_error') else 'transport_invalid';break
            roles=['executor'] if paper=='P1' else (['builder','verifier'] if row['organization']=='short' else ['implementer','tester','reviewer','integrator'])
            if active is not None:tool={'ok':False,'error':'worker_active','worker':active[1]}
            elif starts>=spawn_limit:tool={'ok':False,'error':'worker_start_budget_exhausted'}
            elif action.get('role') not in roles or not isinstance(action.get('task'),str):tool={'ok':False,'error':'invalid_role_or_task'}
            else:
                starts+=1;wid=f'worker-{starts:02d}';request=ep/'workers'/f'{wid}-request.json'
                save(request,{'worker_id':wid,'role':action['role'],'task':action['task'],'row':row})
                stdout=(ep/'workers'/f'{wid}-stdout.txt').open('w');stderr=(ep/'workers'/f'{wid}-stderr.txt').open('w')
                proc=subprocess.Popen([sys.executable,str(ROOT/'runtime.py'),'--worker',str(request),'--episode',str(ep)],cwd=ROOT,stdout=stdout,stderr=stderr,start_new_session=True)
                stdout.close();stderr.close();active=(proc,wid);tool={'ok':True,'worker_started':wid,'pid':proc.pid,'role':action['role']}
                record(ep,'controller','worker_spawn',tool)
        elif op=='wait' and not mono:tool=get_worker(True)
        elif op=='inspect':tool={'files':files_state(ep,True),'worker_state':get_worker(False)}
        elif op=='write_file' and mono:tool=write(ep,'controller','single',action.get('file'),action.get('content'))
        elif op=='run_tests':
            tool=run_tests(ep,'controller',task_id);lastcheck=tool['public']
            if not lastcheck['passed'] and first_failure_budget is None:first_failure_budget={'decisions_remaining':limit-turn,'worker_starts_remaining':spawn_limit-starts}
        elif op=='accept' and not mono:
            tool=acceptance(ep,'controller',task_id,paper);lastcheck=tool['implementation']
            if not tool['passed'] and first_failure_budget is None:first_failure_budget={'decisions_remaining':limit-turn,'worker_starts_remaining':spawn_limit-starts}
        elif op in ['finish','request_external']:
            final_action=action;status='reported_'+str(action.get('status')) if op=='finish' else 'requested_external';break
        else:tool={'ok':False,'error':'invalid_json_or_unauthorized_operation'}
        if tool.get('runtime_error') or tool.get('worker_result',{}).get('transport_error'):status='runtime_invalid' if tool.get('runtime_error') else 'transport_invalid';record(ep,'controller','tool_result',tool);break
        if active is None and before==files_state(ep) and op not in ['spawn']:no_progress+=1
        record(ep,'controller','tool_result',tool);history.append({'role':'tool','content':tool})
    if active is not None and active[0].poll() is not None:
        collected=get_worker(False);record(ep,'controller','completed_worker_collected_at_stop',collected)
        if collected.get('runtime_error') or collected.get('worker_result',{}).get('transport_error'):status='runtime_invalid' if collected.get('runtime_error') else 'transport_invalid'
    active_at_stop=active is not None
    # Evaluate the artifacts at the actual decision boundary, then await any late worker separately.
    frozen_at_stop=ep/'stop-state';snapshot=freeze_artifacts(ep,frozen_at_stop);stop_fs=snapshot['files']
    final=acceptance(frozen_at_stop,'terminal-evaluator',task_id,paper,True)
    late=get_worker(True) if active else None
    final_fs=files_state(ep)
    if late and (late.get('runtime_error') or late.get('worker_result',{}).get('transport_error')):status='runtime_invalid' if late.get('runtime_error') else 'transport_invalid'
    result={'id':row['id'],'row':row,'status':status,'controller_turns':turn,'worker_starts':starts,'final_action':final_action,'independent_pass_at_stop':final['passed'],'autonomous_delivery':status=='reported_complete' and final['passed'],'premature_completion':status=='reported_complete' and not final['passed'],'active_worker_at_stop':active_at_stop,'artifacts_changed_after_stop':stop_fs!=final_fs,'no_active_no_artifact_change_actions':no_progress,'initial_recovery_budget':{'decisions':limit,'worker_starts':spawn_limit},'first_controller_check_failure_budget':first_failure_budget,'final_acceptance':final,'snapshot':snapshot,'worker_summaries':worker_summaries,'controller_actions':controller_actions,'finished_utc':now()}
    result['evaluation_race_possible']=False
    save(ep/'history-controller.json',history);save(ep/'result.json',result);record(ep,'controller','process_finished',{'status':status,'autonomous_delivery':result['autonomous_delivery']})
    return result

if __name__=='__main__':
    ap=argparse.ArgumentParser();ap.add_argument('--worker');ap.add_argument('--seed-task');ap.add_argument('--episode',required=True);ap.add_argument('--row');args=ap.parse_args()
    if args.seed_task:seed_execution(Path(args.episode),args.seed_task)
    elif args.worker:worker(Path(args.episode),args.worker)
    else:episode(json.loads(Path(args.row).read_text()),Path(args.episode))
