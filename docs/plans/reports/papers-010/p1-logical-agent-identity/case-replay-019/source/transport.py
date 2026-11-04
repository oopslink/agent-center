import json,subprocess,time,hashlib
from pathlib import Path
from datetime import datetime,timezone

def now():return datetime.now(timezone.utc).isoformat()
def save(p,v):
    p=Path(p);p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
def parse(text):
    text=text.strip()
    if text.startswith('```') and text.endswith('```'):text='\n'.join(text.splitlines()[1:-1])
    try:
        v=json.loads(text);return v if isinstance(v,dict) else None
    except (ValueError,TypeError):return None

def invoke(model,system,history,outdir):
    outdir=Path(outdir);outdir.mkdir(parents=True,exist_ok=True)
    prompt=json.dumps({'conversation':history},ensure_ascii=False)
    argv=['claude','-p','--safe-mode','--tools','','--disable-slash-commands','--strict-mcp-config','--no-session-persistence','--system-prompt',system,'--model',model,'--effort','low','--output-format','json','--max-budget-usd','0.35']
    for attempt in [1,2]:
        p=outdir/f'attempt-{attempt}';p.mkdir(exist_ok=False)
        save(p/'request.json',{'model_requested':model,'system':system,'conversation':history,'prompt_sha256':hashlib.sha256(prompt.encode()).hexdigest(),'argv':argv,'started_utc':now()})
        start=time.monotonic()
        try:
            r=subprocess.run(argv,input=prompt,capture_output=True,text=True,cwd=Path(__file__).parent/'sandbox',timeout=90)
            stdout,stderr,rc,timeout=r.stdout,r.stderr,r.returncode,False
        except subprocess.TimeoutExpired as e:
            stdout,stderr,rc,timeout=e.stdout or '',e.stderr or '',None,True
            if isinstance(stdout,bytes):stdout=stdout.decode('utf8','replace')
            if isinstance(stderr,bytes):stderr=stderr.decode('utf8','replace')
        (p/'stdout.txt').write_text(stdout);(p/'stderr.txt').write_text(stderr)
        try:payload=json.loads(stdout)
        except ValueError:payload={}
        ok=rc==0 and payload.get('type')=='result' and not payload.get('is_error') and payload.get('subtype')=='success'
        result={'transport_ok':ok,'attempt':attempt,'returncode':rc,'timeout':timeout,'duration_s':time.monotonic()-start,'finished_utc':now(),'model_reported':list(payload.get('modelUsage',{})),'cost_usd_reported':payload.get('total_cost_usd',0),'usage':payload.get('usage',{}),'model_usage':payload.get('modelUsage',{}),'text':payload.get('result',''),'parsed':parse(payload.get('result','')) if ok else None}
        save(p/'metadata.json',result)
        if ok:break
    save(outdir/'result.json',result)
    return result
