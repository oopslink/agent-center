#!/usr/bin/env python3
"""生成 023 正式批次的设计并冻结。冻结在任何正式调用之前完成。"""
import hashlib,json
from pathlib import Path
from tasks023 import TASKS023

ROOT=Path(__file__).resolve().parent
MODELS=['claude-haiku-5-5','claude-sonnet-5-5']
TRANSPORTS=['report_only','shared_receipts','issuer_bound']
BUDGET={'decision_budget':32,'worker_start_budget':12}
DEV=[t for t,v in TASKS023.items() if v['split']=='development']
HOLDOUT=[t for t,v in TASKS023.items() if v['split']=='holdout']
E2_TASKS=['h_sessions','o_buckets']   # 一个开发家族 + 一个留出家族

def rows():
    out=[]
    # 实验一：组织 × 凭据水平 × 模型，每格 n=5（五个家族各一次）
    for org in ['short','long']:
        for tr in TRANSPORTS:
            for model in MODELS:
                for task in DEV+HOLDOUT:
                    out.append({'id':f'e1-{org}-{tr}-{model.split("-")[1]}-{task}',
                                'experiment':'coordination_cost','paper':'P2','task':task,
                                'family':TASKS023[task]['family'],'split':TASKS023[task]['split'],
                                'model':model,'organization':org,'binding':'distributed',
                                'ledger':'off','identity':'separate','transport':tr,
                                **BUDGET,'run_version':'023-main'})
    # 实验二：凭据水平 × 起点 × 模型 × 两个家族，主格为 (transport, seed) 下 n=4
    for tr in TRANSPORTS:
        for seed in ['valid','stale']:
            for model in MODELS:
                for task in E2_TASKS:
                    out.append({'id':f'e2-{tr}-{seed}-{model.split("-")[1]}-{task}',
                                'experiment':'receipt_recognition','paper':'P2','task':task,
                                'family':TASKS023[task]['family'],'split':TASKS023[task]['split'],
                                'model':model,'organization':'long','binding':'distributed',
                                'ledger':'off','identity':'separate','transport':tr,
                                'seed_cross_session':seed,**BUDGET,'run_version':'023-main'})
    return out

if __name__=='__main__':
    runs=rows()
    assert len({r['id'] for r in runs})==len(runs)
    design={'runs':runs,'models':MODELS,'transports':TRANSPORTS,'budget':BUDGET,
            'development_families':DEV,'holdout_families':HOLDOUT,
            'experiment_2_families':E2_TASKS,
            'cells':{'experiment_1':'organization × transport × model, n=5 (five task families)',
                     'experiment_2':'transport × seed, n=4 (two models × two task families)'},
            'primary_endpoints':{'experiment_1':['total_decisions','evidence_cycles'],
                                 'experiment_2':['evidence_cycles','closed_within_budget']},
            'counterexample_endpoint':{'experiment_2_stale':'wrongly_closed'},
            'secondary_endpoints':['autonomous_delivery','independent_pass'],
            'exclusion_rule':'status in (transport_invalid, runtime_invalid) 的 episode 记为无效并排除，原始记录保留',
            'budget_cap_usd':200,'no_post_hoc_retry':True}
    (ROOT/'design023.json').write_text(json.dumps(design,ensure_ascii=False,indent=1)+'\n')
    files=['tasks023.py','receipts.py','runtime.py','analyse023.py','selfcheck.py',
           'build_design023.py','tasks.py','transport.py','check_candidate.py','design023.json']
    lock={'files':{f:hashlib.sha256((ROOT/f).read_bytes()).hexdigest() for f in files},
          'scoring_sha256':hashlib.sha256((ROOT/'analyse023.py').read_bytes()).hexdigest(),
          'runs':len(runs),'budget':BUDGET,
          'experiment_1_runs':sum(1 for r in runs if r['experiment']=='coordination_cost'),
          'experiment_2_runs':sum(1 for r in runs if r['experiment']=='receipt_recognition')}
    (ROOT.parent/'design-lock.json').write_text(json.dumps(lock,ensure_ascii=False,indent=1)+'\n')
    print(f"设计：{len(runs)} 个 episode（实验一 {lock['experiment_1_runs']}、实验二 {lock['experiment_2_runs']}）")
    print('lock 写入 ../design-lock.json')
