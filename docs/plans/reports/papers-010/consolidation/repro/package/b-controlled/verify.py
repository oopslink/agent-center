#!/usr/bin/env python3
"""从本包内的打分结果重算 B 篇正文引用的数字，并逐条断言。

只用标准库。先核对评分哈希，再重算；任一条不符即退出码 1。
用法：python3 verify.py
"""
import collections,hashlib,json,statistics,sys
from pathlib import Path

HERE=Path(__file__).resolve().parent
PAPER={
 'episodes':84,'invalid':0,'reported_cost_usd':26.56276,
 'e1_n':60,'e2_n':24,
 'long_median':28,'long_min':25,'long_max':68,'short_median':19,'short_min':15,'short_max':62,
 'long_cycles_median':5,'short_cycles_median':3,
 'long_delivery':25,'short_delivery':28,
 'paired_org_median':9,'paired_org_min':-26,'paired_org_max':45,'paired_org_positive':28,'paired_org_pairs':30,
 'haiku_org_median':9,'sonnet_org_median':9,'sonnet_org_positive':15,'sonnet_org_min':1,'sonnet_org_max':13,
 'report_only_delivery':13,'shared_delivery':20,'issuer_delivery':20,
 'shared_only_delivered_pairs':7,'report_only_only_delivered_pairs':0,
 'dev_delivery':32,'dev_n':36,'holdout_delivery':21,'holdout_n':24,
 'e2_report_only_valid_median':32.0,'e2_shared_valid_median':11.5,'e2_issuer_valid_median':17.0,
 'e2_report_only_stale_median':30.5,'e2_shared_stale_median':18.0,'e2_issuer_stale_median':15.5,
 'wrongly_closed':0,'stale_independent_pass':12,'stale_n':12,
 'haiku_long_report_only_delivery':0,'haiku_long_report_only_median':40,
}

def load():
    rows=[]
    for p in sorted((HERE/'episodes').glob('*/result.json')):
        r=json.loads(p.read_text(encoding='utf-8'))
        row=r['row'];actions=[a.get('op') if isinstance(a,dict) else None for a in r['controller_actions']]
        wd=sum(w['turns'] for w in r['worker_summaries'])
        rows.append({'experiment':row['experiment'],'task':row['task'],'split':row['split'],
                     'model':row['model'],'organization':row.get('organization'),
                     'transport':row['transport'],
                     'seed':row.get('seed_cross_session') or 'failed_execution',
                     'total_decisions':r['controller_turns']+wd,
                     'evidence_cycles':sum(1 for a in actions if a in ('spawn','run_tests')),
                     'autonomous_delivery':bool(r['autonomous_delivery']),
                     'independent_pass':bool(r['independent_pass_at_stop']),
                     'wrongly_closed':bool(r['premature_completion']),
                     'invalid':r['status'] in ('transport_invalid','runtime_invalid')})
    return rows

def paired(rows,factor,a,b,keys,metric):
    I={}
    for r in rows: I[(tuple(r[k] for k in keys),r[factor])]=r
    out=[]
    for (cell,lv),r in I.items():
        if lv!=b: continue
        base=I.get((cell,a))
        if base: out.append(r[metric]-base[metric])
    return out

def computed():
    rows=load();status=json.loads((HERE/'run-status.json').read_text(encoding='utf-8'))
    e1=[r for r in rows if r['experiment']=='coordination_cost']
    e2=[r for r in rows if r['experiment']=='receipt_recognition']
    def sub(rs,**kw): return [r for r in rs if all(r[k]==v for k,v in kw.items())]
    def td(rs): return [r['total_decisions'] for r in rs]
    long_,short=sub(e1,organization='long'),sub(e1,organization='short')
    po=paired(e1,'organization','short','long',['task','model','transport'],'total_decisions')
    ph=paired(sub(e1,model='claude-haiku-5-5'),'organization','short','long',['task','transport'],'total_decisions')
    ps=paired(sub(e1,model='claude-sonnet-5-5'),'organization','short','long',['task','transport'],'total_decisions')
    I={}
    for r in e1: I[((r['task'],r['model'],r['organization']),r['transport'])]=r
    won=lost=0
    for (cell,lv),r in I.items():
        if lv!='shared_receipts': continue
        base=I.get((cell,'report_only'))
        if not base: continue
        if r['autonomous_delivery'] and not base['autonomous_delivery']: won+=1
        if base['autonomous_delivery'] and not r['autonomous_delivery']: lost+=1
    hl=sub(e1,model='claude-haiku-5-5',organization='long',transport='report_only')
    def e2m(tr,sd): return statistics.median(td(sub(e2,transport=tr,seed=sd)))
    stale=sub(e2,seed='stale')
    return {
     'episodes':len(rows),'invalid':sum(r['invalid'] for r in rows),
     'reported_cost_usd':status['spent_usd'],'e1_n':len(e1),'e2_n':len(e2),
     'long_median':statistics.median(td(long_)),'long_min':min(td(long_)),'long_max':max(td(long_)),
     'short_median':statistics.median(td(short)),'short_min':min(td(short)),'short_max':max(td(short)),
     'long_cycles_median':statistics.median([r['evidence_cycles'] for r in long_]),
     'short_cycles_median':statistics.median([r['evidence_cycles'] for r in short]),
     'long_delivery':sum(r['autonomous_delivery'] for r in long_),
     'short_delivery':sum(r['autonomous_delivery'] for r in short),
     'paired_org_median':statistics.median(po),'paired_org_min':min(po),'paired_org_max':max(po),
     'paired_org_positive':sum(1 for x in po if x>0),'paired_org_pairs':len(po),
     'haiku_org_median':statistics.median(ph),'sonnet_org_median':statistics.median(ps),
     'sonnet_org_positive':sum(1 for x in ps if x>0),'sonnet_org_min':min(ps),'sonnet_org_max':max(ps),
     'report_only_delivery':sum(r['autonomous_delivery'] for r in sub(e1,transport='report_only')),
     'shared_delivery':sum(r['autonomous_delivery'] for r in sub(e1,transport='shared_receipts')),
     'issuer_delivery':sum(r['autonomous_delivery'] for r in sub(e1,transport='issuer_bound')),
     'shared_only_delivered_pairs':won,'report_only_only_delivered_pairs':lost,
     'dev_delivery':sum(r['autonomous_delivery'] for r in sub(e1,split='development')),
     'dev_n':len(sub(e1,split='development')),
     'holdout_delivery':sum(r['autonomous_delivery'] for r in sub(e1,split='holdout')),
     'holdout_n':len(sub(e1,split='holdout')),
     'e2_report_only_valid_median':e2m('report_only','valid'),
     'e2_shared_valid_median':e2m('shared_receipts','valid'),
     'e2_issuer_valid_median':e2m('issuer_bound','valid'),
     'e2_report_only_stale_median':e2m('report_only','stale'),
     'e2_shared_stale_median':e2m('shared_receipts','stale'),
     'e2_issuer_stale_median':e2m('issuer_bound','stale'),
     'wrongly_closed':sum(r['wrongly_closed'] for r in e2),
     'stale_independent_pass':sum(r['independent_pass'] for r in stale),'stale_n':len(stale),
     'haiku_long_report_only_delivery':sum(r['autonomous_delivery'] for r in hl),
     'haiku_long_report_only_median':statistics.median(td(hl)),
    }

def main():
    lock=json.loads((HERE/'design-lock.json').read_text(encoding='utf-8'))
    actual=hashlib.sha256((HERE/'scripts/analyse023.py').read_bytes()).hexdigest()
    if lock.get('scoring_sha256')!=actual:
        print(f"评分映射与 design-lock 不一致：{lock.get('scoring_sha256')} vs {actual}",file=sys.stderr);return 1
    print(f"评分映射哈希与 design-lock 一致：{actual[:16]}…\n")
    got=computed();bad=[]
    width=max(len(k) for k in PAPER)
    for k,want in PAPER.items():
        have=got.get(k);ok=have==want
        if not ok: bad.append((k,want,have))
        print(f"{'ok ' if ok else 'BAD'} {k:<{width}} 论文 {want!s:>10}   重算 {have!s:>10}")
    print(f"\n{len(PAPER)-len(bad)}/{len(PAPER)} 条一致")
    for k,want,have in bad: print(f'  不符 {k}: 论文 {want}，重算 {have}',file=sys.stderr)
    return 1 if bad else 0

if __name__=='__main__': sys.exit(main())
