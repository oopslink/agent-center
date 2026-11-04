#!/usr/bin/env python3
"""配对汇总。读 analysis.json 的逐 episode 终点，不重新打分。

本脚本在看到分格汇总之后写成。终点定义与评分映射在任何正式调用之前就已冻结
（design-lock.json 的 scoring_sha256），配对只改变汇总方式，不改变任何 episode
的打分，也不引入新的终点。
"""
import json,statistics,sys
from pathlib import Path

def paired(rows,factor,a,b,keys,metrics):
    """在 keys 相同的单元内配对比较 factor 的两个水平。"""
    index={}
    for r in rows:
        index.setdefault((tuple(r[k] for k in keys),r[factor]),[]).append(r)
    out=[]
    for (cell,level),items in list(index.items()):
        if level!=b:continue
        other=index.get((cell,a))
        if not other:continue
        for m in metrics:
            out.append({'cell':dict(zip(keys,cell)),'metric':m,
                        a:other[0][m],b:items[0][m],
                        'delta':items[0][m]-other[0][m]})
    return out

def summarise(deltas,metric):
    vals=[d['delta'] for d in deltas if d['metric']==metric]
    if not vals:return None
    return {'pairs':len(vals),'median':statistics.median(vals),
            'range':[min(vals),max(vals)],
            'positive':sum(1 for v in vals if v>0),
            'zero':sum(1 for v in vals if v==0),
            'negative':sum(1 for v in vals if v<0),
            'sum':sum(vals)}

def main():
    src=Path(sys.argv[1] if len(sys.argv)>1 else '../analysis.json')
    rows=json.load(open(src))['rows']
    e1=[r for r in rows if r['experiment']=='coordination_cost' and not r['invalid_run']]
    e2=[r for r in rows if r['experiment']=='receipt_recognition' and not r['invalid_run']]
    METRICS=['total_decisions','controller_decisions','worker_decisions','evidence_cycles']
    report={'experiment_1':{},'experiment_2':{}}
    # 组织：四角色 − 两角色，在 (task, model, transport) 内配对
    d=paired(e1,'organization','short','long',['task','model','transport'],METRICS)
    report['experiment_1']['long_minus_short']={m:summarise(d,m) for m in METRICS}
    report['experiment_1']['long_minus_short_pairs']=d
    # 凭据水平：以 report_only 为基线，在 (task, model, organization) 内配对
    for level in ('shared_receipts','issuer_bound'):
        d=paired(e1,'transport','report_only',level,['task','model','organization'],METRICS)
        report['experiment_1'][f'{level}_minus_report_only']={m:summarise(d,m) for m in METRICS}
    # 实验二：同上，另加索证轮数
    for level in ('shared_receipts','issuer_bound'):
        d=paired(e2,'transport','report_only',level,['task','model','seed'],
                 ['evidence_cycles','total_decisions'])
        report['experiment_2'][f'{level}_minus_report_only']={
            m:summarise(d,m) for m in ['evidence_cycles','total_decisions']}
    out=src.parent/'paired.json'
    out.write_text(json.dumps(report,ensure_ascii=False,indent=1)+'\n')
    print(f'写入 {out}')
    for exp in ('experiment_1','experiment_2'):
        for comp,v in report[exp].items():
            if comp.endswith('_pairs'):continue
            s=v.get('total_decisions') or v.get('evidence_cycles')
            if not s:continue
            print(f"{exp} {comp}: 总决策差 中位 {v['total_decisions']['median']} "
                  f"范围 {v['total_decisions']['range']} 正/零/负 "
                  f"{v['total_decisions']['positive']}/{v['total_decisions']['zero']}/{v['total_decisions']['negative']} "
                  f"（{v['total_decisions']['pairs']} 对）")
    return 0

if __name__=='__main__':sys.exit(main())
