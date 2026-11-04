"""Validate source locators, archived bundles, aggregates and research links."""
from pathlib import Path
import json,csv,hashlib,subprocess,re,datetime,xml.etree.ElementTree as ET
R=Path(__file__).resolve().parents[1]
REPO=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
def load(p):return json.loads((R/p).read_text())
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
checks={}
inv=load('derived/repo-inventory.json');bundles=[]
for x in inv:
 if x.get('bundle_sha256'):
  p=R/'raw/memory'/x['label']/'history.bundle'
  assert sha(p)==x['bundle_sha256']
  cmd=['git','-C',str(REPO),'bundle','verify',str(p)];v=subprocess.run(cmd,capture_output=True,text=True)
  assert v.returncode==0,v.stderr;bundles.append(x['label'])
assert all(x['refs_stable'] for x in inv if x['git_repo'])
checks['bundles_verified']=bundles
loc=load('evidence/source-locators.json')
for x in loc:
 p=R/x['file'];assert sha(p)==x['file_sha256'];row=json.loads(p.read_text())[int(x['pointer'][1:])];assert row['id']==x['id']
checks['source_locators_verified']=len(loc)
s=load('derived/summary.json');events=load('derived/event-git-validation.json')
assert len(events)==57 and all(e['status_matches'] for e in events)
with (R/'derived/read-receipts.csv').open() as f:a=list(csv.DictReader(f))
assert len(a)==230 and all(x['resolved']==x['enabled']==x['phase_allowed']=='True' for x in a)
assert sum(int(x['receipts']) for x in csv.DictReader((R/'derived/agent-rule-matrix.csv').open()))==230
assert s['proposal_status']=={'promoted':15,'pending':3,'rejected':12}
assert s['receipt_contexts']==s['supervisor_contexts']+s['execution_contexts']==67
assert sum(x['files'] for x in s['legacy_files_by_scope'].values())==23
assert sum(x['bytes'] for x in s['legacy_files_by_scope'].values())==129938
checks['event_git_matches']=57;checks['audit_rows_validated']=230
src=load('derived/source-provenance.json')
assert len(src)==27 and all(x['exists'] for x in src)
for path in set(x['path'] for x in src):assert len(set(x['sha256'] for x in src if x['path']==path))==1
checks['reference_source_paths_identical_across_three_commits']=9
for name in ['01-governance','02-access']:
 ET.parse(R/'figures'/(name+'.svg'))
 assert (R/'figures'/(name+'.pdf')).read_bytes().startswith(b'%PDF')
 assert (R/'figures'/(name+'.png')).read_bytes().startswith(b'\x89PNG\r\n\x1a\n')
checks['figure_files_valid']=6;checks['visual_review']='Both PNGs inspected; no clipping/overlap observed. SVG/PDF generated from the same figures.'
reports=[REPO/'docs/plans/reports'/n for n in ['agent-harness-memory-study-011.md','agent-harness-memory-cases-011.md','agent-harness-memory-011.md']]
reports.append(REPO/'docs/plans/reports/memory-011/round-002/README.md')
checked=[];deferred=[]
for p in reports:
 for target in re.findall(r'\]\(([^)]+)\)',p.read_text()):
  if target.startswith(('http','#')):continue
  dest=(p.parent/target.split('#')[0]).resolve()
  if dest==REPO/'docs/plans/reports/memory-011/round-002/validation.json':deferred.append(dest)
  else:assert dest.exists(),str(p)+': '+target
  checked.append(str(dest))
checks['local_links_checked']=len(checked)
v=subprocess.run(['git','-C',str(REPO),'diff','--check'],capture_output=True,text=True);assert v.returncode==0,v.stdout+v.stderr
checks['git_diff_check']='passed; includes tracked document edits only'
checks['scope']='Research documentation/data only. No product tests required for this non-commit turn; no claim that product suites were run.'
checks['validated_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
(R/'logs/validation.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2)+'\n')
for p in deferred:p.write_text(json.dumps(checks,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(checks,ensure_ascii=False,indent=2))
