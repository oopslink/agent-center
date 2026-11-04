"""Bounded local evidence checks, safe mirror, and immutable revision manifest."""
from pathlib import Path
import json,re,hashlib,subprocess,shutil,datetime as dt,xml.etree.ElementTree as ET
from urllib.parse import unquote
R=Path('/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/014-p1-longitudinal')
W=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
M=Path('/Users/oopslink/works/codes/oopslink/agent-center')
REL=Path('docs/plans/reports/papers-010/p1-logical-agent-identity')
P=W/REL;D=P/'longitudinal-014'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def dump(p,x):
 p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
def load(p):return json.loads(p.read_text())
def stamp():return dt.datetime.now(dt.timezone.utc).isoformat()
checks={};records=load(R/'derived/record-locators.json')
for x in records:
 p=R/x['file'];assert sha(p)==x['sha256'];assert load(p)['id']==x['id']
checks['native_message_hashes_checked']=len(records)
checks['case_groups']=len(load(R/'derived/cases.json'))
checks['september_task_rows']=len(load(R/'raw/pm_tasks-september.json'))
checks['september_action_rows']=len(load(R/'raw/pm_task_action_logs-september.json'))
for x in load(R/'logs/protected-experiment-files.json'):assert sha(Path(x['path']))==x['sha256']
checks['protected_original_experiment_files_unchanged']=True
for name in ['experiment-data.json','experiment-results.csv','experiment-provenance.json']:
 original=subprocess.check_output(['git','show','HEAD:'+str(REL/name)],cwd=W)
 assert original==(P/name).read_bytes(),name
checks['repository_experiment_artifacts_unchanged']=True
for fig in range(1,5):
 for p in (P/'figures').glob(f'p1-fig{fig}-*'):
  assert subprocess.check_output(['git','show','HEAD:'+str(p.relative_to(W))],cwd=W)==p.read_bytes()
checks['original_figures_1_to_4_unchanged']=True
def refs(s):return s.split('## 参考文献',1)[1].split('## 证据附录',1)[0]
assert refs((R/'before'/REL/'paper.md').read_text())==refs((P/'paper.md').read_text())
checks['references_byte_identical']=True
t=load(D/'derived/timeline.json')
assert t['seconds_task_creation_before_identity_reminder']>0
assert t['seconds_created_event_before_identity_reminder']==1.614726
assert t['seconds_action_instruction_to_created_event']==53.185624
checks['both_native_clocks_preserve_counterevidence']=True
fig=P/'figures/p1-fig5-responsibility-continuity'
assert fig.with_suffix('.png').read_bytes().startswith(b'\x89PNG\r\n\x1a\n')
assert fig.with_suffix('.pdf').read_bytes().startswith(b'%PDF')
ET.parse(fig.with_suffix('.svg'))
checks['figure_formats_valid']=True
checks['figure_visual_check']='Inspected rendered PNG after clock clarification: no clipping or legend overlap; selected records are not an incidence curve.'
modified=[p.relative_to(R/'before') for p in (R/'before').rglob('*') if p.is_file()]
extra=[REL/'longitudinal-analysis.md',REL/'REVISION-014.md',REL/'revision-014.diff']
extra += [p.relative_to(W) for p in D.rglob('*') if p.is_file()]
extra += [p.relative_to(W) for p in (P/'figures').glob('p1-fig5-*')]
files=sorted(set(modified+extra),key=str)
# Link resolution maps main-worktree targets to the prepared research copies.
missing=[];checked=0
for rel in files:
 if rel.suffix!='.md':continue
 p=W/rel
 for target in re.findall(r'!?\[[^\]\n]*\]\(([^)\n]+)\)',p.read_text()):
  if target.startswith(('https://','http://','#','mailto:')):continue
  target=re.sub(r':\d+$','',unquote(target.split('#',1)[0].strip('<>')))
  path=Path(target) if target.startswith('/') else p.parent/target
  try:path=W/path.relative_to(M)
  except ValueError:pass
  checked+=1
  if not path.exists() and path != D/'validation.json':missing.append({'file':str(rel),'target':target})
checks['local_links_checked']=checked;checks['missing_local_links']=missing
assert not missing,missing
subprocess.run(['git','diff','--check'],cwd=W,check=True,capture_output=True)
checks['git_diff_check']=True
closure=R/'review-closure.json';assert closure.exists(),'Wait for reviewer closure.'
shutil.copy2(closure,D/'review-closure.json')
checks['bounded_ai_review_closure']=load(closure)
result={'checked_utc':stamp(),'paper_sha256':sha(P/'paper.md'),'checks':checks,'limitations':['Not a human review or independent replication.','No new literature or retraction search; prior verification remains date-bounded.','No product tests: documentation and research artifacts only, no commit in this turn.','Official skill ARS/acronym scripts unavailable; no such gate claimed.','Private full-text source records stay in local archive.']}
dump(R/'validation.json',result);dump(D/'validation.json',result)
# Refresh file set now that closure and validation exist.
files=sorted(set(files+[p.relative_to(W) for p in D.rglob('*') if p.is_file()]),key=str)
preflight=[]
for rel in files:
 dst=M/rel;src=W/rel;old=R/'before'/rel
 if dst.exists():
  allowed=[sha(src)]+([sha(old)] if old.exists() else [])
  assert sha(dst) in allowed,'Concurrent main-worktree change: '+str(rel)
 preflight.append({'path':str(rel),'previous_main_sha256':sha(dst) if dst.exists() else None,'prepared_sha256':sha(src)})
for rel in files:
 dst=M/rel;dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(W/rel,dst)
 assert sha(dst)==sha(W/rel)
 archive=R/'after'/rel;archive.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(W/rel,archive)
dump(R/'logs/mirror.json',{'utc':stamp(),'files':preflight,'scope':'Only this P1 revision plus papers index; existing P5 and main conflict files not touched.'})
manifest=[]
for p in sorted(R.rglob('*')):
 if p.is_file() and p.name!='manifest.json':manifest.append({'path':str(p.relative_to(R)),'bytes':p.stat().st_size,'sha256':sha(p)})
dump(R/'manifest.json',{'created_utc':stamp(),'files':manifest,'large_frozen_database_not_rehashed':True})
print(json.dumps({'status':'passed','paper_sha256':result['paper_sha256'],'local_links':checked,'mirrored_files':len(files),'manifest_files':len(manifest)},ensure_ascii=False))
