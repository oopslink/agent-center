#!/usr/bin/env python3
"""从本包内的数据重算 A 篇正文引用的数字，并与论文所述值逐条断言。

只用标准库。任一条不符即退出码 1 并指出差异。
用法：python3 verify.py
"""
import collections,csv,json,sys
from pathlib import Path

HERE=Path(__file__).resolve().parent
PAPER={  # A 篇正文所述值
 'transitions':169,'topology_changed':166,'topology_unchanged':3,
 'parent_retained':149,'became_completed_changed':101,'became_completed_unchanged':1,
 'parent_retained_pct':88.2,'loo_min_pct':85.4,'loo_max_pct':91.1,
 'receipts':230,'receipt_agents':12,'receipt_commits':8,'receipt_teams':2,
 'versioned_rules':34,'versioned_multi_agent':30,'slug_rules':10,'slug_multi_agent':9,
 'contexts':67,'planning_contexts':63,'execution_contexts':4,
 'phase_execute':125,'phase_plan':38,'phase_review':40,'phase_recovery':27,
 'proposals':30,'reviewed':27,'pending':3,'promoted':15,'rejected':12,
 'self_reviewed':16,'events':57,'canonical_rules':10,'canonical_entries':5,
 'stale_commit_receipts':0}

def rows(p): return list(csv.DictReader(open(HERE/p,encoding='utf-8')))

def computed():
    t=rows('task-graph/p4-transitions.csv')
    ch=[r for r in t if r['topology_changed'].strip().lower()=='true']
    un=[r for r in t if r['topology_changed'].strip().lower()=='false']
    ret=sum(1 for r in t if r['all_parent_nodes_retained'].strip().lower()=='true')
    loo=[float(r['frac']) for r in rows('task-graph/p4-leave-one-plan-out.csv')]
    rc=rows('rule-audit/read-receipts.csv')
    vr=collections.defaultdict(set); bs=collections.defaultdict(set)
    for x in rc:
        vr[(x['team_id'],x['team_memory_commit'],x['rule_slug'])].add(x['agent_id'])
        bs[x['rule_slug']].add(x['agent_id'])
    ph=collections.Counter(x['phase'] for x in rc)
    ctx=set()
    for x in rc: ctx.add(x['execution_id'] or x['planning_session_id'])
    s=json.load(open(HERE/'rule-audit/summary.json',encoding='utf-8'))
    return {
     'transitions':len(t),'topology_changed':len(ch),'topology_unchanged':len(un),
     'parent_retained':ret,
     'became_completed_changed':sum(1 for r in ch if int(r['became_completed'])>0),
     'became_completed_unchanged':sum(1 for r in un if int(r['became_completed'])>0),
     'parent_retained_pct':round(100*ret/len(t),1),
     'loo_min_pct':round(100*min(loo),1),'loo_max_pct':round(100*max(loo),1),
     'receipts':len(rc),'receipt_agents':len({x['agent_id'] for x in rc}),
     'receipt_commits':len({x['team_memory_commit'] for x in rc}),
     'receipt_teams':len({x['team_id'] for x in rc}),
     'versioned_rules':len(vr),'versioned_multi_agent':sum(1 for v in vr.values() if len(v)>=2),
     'slug_rules':len(bs),'slug_multi_agent':sum(1 for v in bs.values() if len(v)>=2),
     'contexts':len(ctx),
     'planning_contexts':s['supervisor_contexts'],'execution_contexts':s['execution_contexts'],
     'phase_execute':ph['execute'],'phase_plan':ph['plan'],
     'phase_review':ph['review'],'phase_recovery':ph['recovery'],
     'proposals':s['proposals'],'reviewed':s['review_minutes_all']['n'],
     'pending':s['proposal_status']['pending'],'promoted':s['proposal_status']['promoted'],
     'rejected':s['proposal_status']['rejected'],'self_reviewed':s['self_reviewed'],
     'events':s['events'],'canonical_rules':s['canonical_rules'],
     'canonical_entries':s['canonical_entries'],
     'stale_commit_receipts':s['older_repo_commit_receipts']}

def main():
    got=computed();bad=[]
    for k,want in PAPER.items():
        have=got.get(k)
        if have!=want: bad.append((k,want,have))
    width=max(len(k) for k in PAPER)
    for k,want in PAPER.items():
        mark='ok ' if got.get(k)==want else 'BAD'
        print(f'{mark} {k:<{width}} 论文 {want!s:>8}   重算 {got.get(k)!s:>8}')
    print(f'\n{len(PAPER)-len(bad)}/{len(PAPER)} 条一致')
    for k,want,have in bad: print(f'  不符 {k}: 论文 {want}，重算 {have}',file=sys.stderr)
    return 1 if bad else 0

if __name__=='__main__': sys.exit(main())
