"""Read-only current binary identity capture; do not overwrite earlier build-info files."""
from pathlib import Path
import hashlib,json,subprocess,datetime
R=Path(__file__).resolve().parents[1]
roots={'center':Path('/Users/oopslink/.agent-center'),'worker':Path('/Users/oopslink/.agent-center/workers/worker-edb09a0c')}
rows=[]
for side,root in roots.items():
 p=(root/'current/bin/agent-center').resolve()
 def sha():
  h=hashlib.sha256()
  with p.open('rb') as f:
   for b in iter(lambda:f.read(1024*1024),b''):h.update(b)
  return h.hexdigest()
 first=sha();v=subprocess.run(['go','version','-m',str(p)],capture_output=True,text=True);assert v.returncode==0
 dst=R/'raw'/(side+'-binary-build-bound.txt');dst.write_text(v.stdout)
 rows.append({'side':side,'resolved_binary':str(p),'bytes':p.stat().st_size,'sha256':first,'sha256_stable_after_build_info':sha()==first,
 'build_info_path':str(dst.relative_to(R)),'build_info_sha256':hashlib.sha256(dst.read_bytes()).hexdigest(),
 'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'evidence_scope':'file on disk, not proof of active process or historical deployment'})
(R/'derived/binary-identities.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
assert all(x['sha256_stable_after_build_info'] for x in rows)
print(json.dumps([{k:x[k] for k in ['side','bytes','sha256','sha256_stable_after_build_info']} for x in rows]))
