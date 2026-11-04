"""Executed in an isolated Python subprocess against actual candidate code."""
import ast,json,sys,copy
from pathlib import Path

def run(module,cases):
    source=Path(module).read_text();tree=ast.parse(source)
    allowed={'math','re','datetime','collections','heapq','decimal','itertools','functools','typing','fractions'}
    for n in ast.walk(tree):
        if isinstance(n,(ast.Import,ast.ImportFrom)):
            names=[x.name.split('.')[0] for x in n.names] if isinstance(n,ast.Import) else [(n.module or '').split('.')[0]]
            if any(x not in allowed for x in names):return {'passed':False,'error':'unsupported_import','imports':names}
        if isinstance(n,ast.Call) and isinstance(n.func,ast.Name) and n.func.id in {'open','exec','eval','compile','__import__','input'}:
            return {'passed':False,'error':'unsupported_io_or_dynamic_code'}
    scope={};exec(compile(tree,str(module),'exec'),scope)
    results=[]
    for i,case in enumerate(cases):
        value=copy.deepcopy(case['input']);before=copy.deepcopy(value)
        try:
            actual=scope['solve'](value);observed={'output':actual}
            valid='output' in case and actual==case['output'] and value==before
        except Exception as e:
            observed={'error':type(e).__name__};valid=case.get('error')==type(e).__name__ and value==before
        results.append({'index':i,'input':case['input'],'expected':{k:v for k,v in case.items() if k!='input'},'observed':observed,'passed':bool(valid),'input_mutated':value!=before})
    return {'passed':all(x['passed'] for x in results),'passed_count':sum(x['passed'] for x in results),'total':len(results),'checks':results}

if __name__=='__main__':
    try: out=run(sys.argv[1],json.loads(Path(sys.argv[2]).read_text()))
    except Exception as e: out={'passed':False,'error':type(e).__name__,'message':str(e)}
    print(json.dumps(out,ensure_ascii=False))
