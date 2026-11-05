#!/usr/bin/env python3
"""从本包内的数据重算 A 篇正文引用的数字，并与论文所述值逐条断言。

只用标准库。任一条不符即退出码 1 并指出差异。
用法：python3 verify.py
"""
import collections,csv,json,math,re,sys
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
 'stale_commit_receipts':0,
 # §3.4 两轮盲编码。第一轮判据四类，第二轮判据五类、抽样框加机械过滤。
 'r1_n_compared':23,'r1_agreement':0.130,'r1_kappa':-0.100,
 'r2_n':15,'r2_agreement':0.667,'r2_kappa':0.540,
 'r2_kappa_lo':0.211,'r2_kappa_hi':0.869}

ROW=re.compile(r'^\|\s*(\d+)\s*\|')

def coding(round_no,categories):
    """读该轮的人工表与 AI 侧编码，返回 (po, kappa, n) —— 与 agreement.py 同一算法。"""
    base=HERE/'blind-coding'/f'round-{round_no}'
    human={}
    for line in (base/'sheet.md').read_text(encoding='utf-8').splitlines():
        if not ROW.match(line): continue
        cells=[c.strip() for c in line.strip().strip('|').split('|')]
        if len(cells)<6: continue
        human[cells[0]]=cells[5].upper()
    ai={k:v['verdict'].upper() for k,v in
        json.load(open(base/'ai-coding.json',encoding='utf-8'))['codes'].items()}
    pairs=[(human[r],ai[r]) for r in sorted(set(human)&set(ai),key=int)
           if human[r] in categories and ai[r] in categories]
    n=len(pairs)
    po=sum(1 for h,a in pairs if h==a)/n
    pe=sum((sum(1 for h,_ in pairs if h==c)/n)*(sum(1 for _,a in pairs if a==c)/n)
           for c in categories)
    k=(po-pe)/(1-pe)
    se=math.sqrt(po*(1-po)/(n*(1-pe)**2))
    return po,k,n,se

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
    r1po,r1k,r1n,_r1se=coding(1,('A','B','C','D'))
    r2po,r2k,r2n,r2se=coding(2,('A','B','C','D','E'))
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
     'stale_commit_receipts':s['older_repo_commit_receipts'],
     'r1_n_compared':r1n,'r1_agreement':round(r1po,3),'r1_kappa':round(r1k,3),
     'r2_n':r2n,'r2_agreement':round(r2po,3),'r2_kappa':round(r2k,3),
     'r2_kappa_lo':round(r2k-1.96*r2se,3),'r2_kappa_hi':round(r2k+1.96*r2se,3)}

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
