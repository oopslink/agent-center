#!/usr/bin/env python3
"""变异验证：逐个破坏关键判定，断言相应检查确实会失败，然后还原。

动机：终检指出"三次变异验证"在材料里没有留痕。本脚本把变异做成可复跑、
有输出的步骤——一个只会在正确实现上通过、却在被破坏的实现上也通过的检查，
等于没有检查。

用法：python3 mutation_check.py [--json]
退出码 0 表示每个变异都被相应检查抓到。
"""
import argparse,json,re,shutil,subprocess,sys,tempfile
from pathlib import Path

ROOT=Path(__file__).resolve().parent
PYTEST=['uv','run','--with','pytest','python','-m','pytest','-q','tests/','test_runtime.py']

MUTATIONS=[
 {'id':'selfcheck-boundary','file':'tasks023.py','why':'破坏会话切分的严格大于语义',
  'old':"if cur is None or t-cur['end']>gap:",'new':"if cur is None or t-cur['end']>=gap:",
  'check':['python3','selfcheck.py'],'expect':'reference 与手写期望不一致'},
 {'id':'selfcheck-list-merge','file':'tasks023.py','why':'把配置合并的列表整体替换改成拼接',
  'old':"                elif kind(b[k])==kind(p[k]):out[k]=p[k]",
  'new':"                elif kind(b[k])=='list' and kind(p[k])=='list':out[k]=b[k]+p[k]\n                elif kind(b[k])==kind(p[k]):out[k]=p[k]",
  'check':['python3','selfcheck.py'],'expect':'reference 与手写期望不一致'},
 {'id':'selfcheck-seed-correct','file':'tasks023.py','why':'让错误起点变正确，失败空间消失',
  'old':"'seed': \"def solve(data):\\n    w=data['width']\\n    return [{'bucket':t//w,'start':t,'count':1} for t in data['requests']]\\n\"",
  'new':"'seed': '''def solve(data):\\n    w=data['width']\\n    if w<1:raise ValueError('width')\\n    c={}\\n    for t in data['requests']:c[t//w]=c.get(t//w,0)+1\\n    return [{'bucket':b,'start':b*w,'count':c[b]} for b in sorted(c)]\\n'''",
  'check':['python3','selfcheck.py'],'expect':'没有失败空间'},
 {'id':'receipts-visibility','file':'receipts.py','why':'让回执列表忽略可见性水平',
  'old':'    if transport == "report_only":\n        return {"ok": False, "error": "receipt_listing_unavailable"}\n',
  'new':'','check':PYTEST,'expect':'test_report_only_hides_the_worker_receipt_from_the_controller'},
 {'id':'receipts-issuer','file':'receipts.py','why':'让可接受性忽略签发者约束',
  'old':'    if required_issuer is not None and receipt.get("issuer") != required_issuer:\n        return False, "issuer_not_permitted"\n',
  'new':'','check':PYTEST,'expect':'test_cross_issuer_receipt_is_refused_under_issuer_bound'},
]

def run(cmd):
    env={'PYTHONDONTWRITEBYTECODE':'1'}
    import os
    e=dict(os.environ); e.update(env)
    p=subprocess.run(cmd,cwd=ROOT,capture_output=True,text=True,env=e,timeout=600)
    return p.returncode,(p.stdout or '')+(p.stderr or '')

def main():
    ap=argparse.ArgumentParser();ap.add_argument('--json',action='store_true');a=ap.parse_args()
    results=[]
    for m in MUTATIONS:
        target=ROOT/m['file'];backup=Path(tempfile.mkdtemp())/m['file']
        shutil.copy2(target,backup)
        try:
            text=target.read_text(encoding='utf-8')
            if m['old'] not in text:
                results.append({**{k:m[k] for k in ('id','file','why')},
                                'caught':False,'reason':'变异锚点不存在，可能源码已改'})
                continue
            target.write_text(text.replace(m['old'],m['new'],1),encoding='utf-8')
            code,out=run(m['check'])
            caught=code!=0 and m['expect'] in out
            results.append({**{k:m[k] for k in ('id','file','why')},'caught':caught,
                            'exit_code':code,'matched_expectation':m['expect'] in out})
        finally:
            shutil.copy2(backup,target)
    missed=[r for r in results if not r['caught']]
    if a.json:print(json.dumps(results,ensure_ascii=False,indent=1))
    else:
        for r in results:
            print(f"{'抓到' if r['caught'] else '漏掉'} {r['id']:<24} {r['file']:<14} {r['why']}")
        print(f"\n{len(results)-len(missed)}/{len(results)} 个变异被相应检查抓到")
    return 1 if missed else 0

if __name__=='__main__':sys.exit(main())
