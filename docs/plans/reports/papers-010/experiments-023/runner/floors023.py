#!/usr/bin/env python3
"""结构下界分析。**本脚本在看到分格汇总之后写成**，属事后分析。

动机（来自终检）：主终点 total_decisions 把每个角色被迫产生的 spawn、wait 与该
worker 的回合都计入，因此角色数本身就给 total_decisions 设了一个下界。若不扣掉
这个下界，"四角色比两角色多 N 次决策"读起来像协调成本的测量，实际主要是终点
定义与因子水平之间的记数恒等式。

本脚本做三件事：报出各臂的观测下界、报出扣掉下界后的配对差、并把两者并列，
使读者能自己判断"多出的部分"有多少来自结构、多少来自行为。
"""
import json,statistics,sys
from pathlib import Path

KEYS=('worker_starts','controller_decisions','worker_decisions','total_decisions')

def main():
    src=Path(sys.argv[1] if len(sys.argv)>1 else '../analysis.json')
    rows=[r for r in json.load(open(src))['rows']
          if r['experiment']=='coordination_cost' and not r['invalid_run']]
    arms={}
    for org in ('short','long'):
        it=[r for r in rows if r['organization']==org]
        arms[org]={k:{'floor':min(r[k] for r in it),'median':statistics.median([r[k] for r in it]),
                      'max':max(r[k] for r in it),'n':len(it)} for k in KEYS}
    I={}
    for r in rows: I[((r['task'],r['model'],r['transport']),r['organization'])]=r
    raw={k:[] for k in KEYS}; excess={k:[] for k in KEYS}
    for (cell,lv),r in I.items():
        if lv!='long': continue
        b=I.get((cell,'short'))
        if not b: continue
        for k in KEYS:
            raw[k].append(r[k]-b[k])
            excess[k].append((r[k]-arms['long'][k]['floor'])-(b[k]-arms['short'][k]['floor']))
    def desc(v):
        return {'pairs':len(v),'median':statistics.median(v),'range':[min(v),max(v)],
                'positive':sum(1 for x in v if x>0),'zero':sum(1 for x in v if x==0),
                'negative':sum(1 for x in v if x<0)}
    report={'note':'事后分析：在看到分格汇总之后写成。',
            'arms':arms,
            'structural_floor_difference':{k:arms['long'][k]['floor']-arms['short'][k]['floor'] for k in KEYS},
            'paired_raw_difference':{k:desc(raw[k]) for k in KEYS},
            'paired_excess_over_floor':{k:desc(excess[k]) for k in KEYS}}
    out=src.parent/'floors.json'
    out.write_text(json.dumps(report,ensure_ascii=False,indent=1)+'\n')
    print(f'写入 {out}\n')
    for k in KEYS:
        f=report['structural_floor_difference'][k]
        print(f"{k:<22} 下界 short={arms['short'][k]['floor']:<3} long={arms['long'][k]['floor']:<3} "
              f"下界差={f:<4} 配对原始差中位={raw and statistics.median(raw[k]):<5} "
              f"扣下界后中位={statistics.median(excess[k]):<5} 正/零/负="
              f"{desc(excess[k])['positive']}/{desc(excess[k])['zero']}/{desc(excess[k])['negative']}")
    return 0

if __name__=='__main__': sys.exit(main())
