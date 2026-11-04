"""Export only curated derived artifacts; retain all raw materials outside Git."""
from pathlib import Path
import shutil,json,hashlib,datetime,difflib
R=Path(__file__).resolve().parents[1]
REPO=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
OUT=REPO/'docs/plans/reports/memory-011/round-002'
def copy(src,dst):dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,dst)
for pat in ['derived/*.csv','derived/summary.json','derived/event-git-validation.json','derived/source-provenance.json','derived/source-milestones.json','derived/deployment-source-matrix.json','derived/binary-identities.json','evidence/*.json','evidence/*.csv','scripts/*.py','figures/*']:
 for p in sorted(R.glob(pat)):copy(p,OUT/p.relative_to(R))
copy(R/'reports/process.md',OUT/'process.md')
copy(R/'protocol.md',OUT/'protocol.md')
if (R/'logs/validation.json').exists():copy(R/'logs/validation.json',OUT/'validation.json')
rels=['docs/plans/README.md','docs/plans/01-agent-harness-research.md']+[str(p.relative_to(REPO)) for p in (REPO/'docs/plans/reports').glob('agent-harness-memory-*.md')]
for rel in rels:
 p=REPO/rel;copy(p,R/'after'/rel)
 old=R/'before'/rel
 before=old.read_text().splitlines(True) if old.exists() else []
 diff=''.join(difflib.unified_diff(before,p.read_text().splitlines(True),fromfile='before/'+rel,tofile='after/'+rel))
 dest=R/'reports/diffs'/(Path(rel).name+'.diff');dest.parent.mkdir(parents=True,exist_ok=True);dest.write_text(diff)
copy(OUT/'README.md',R/'reports/package-README.md')
manifest=[]
for p in sorted(R.rglob('*')):
 if not p.is_file() or 'repo.git' in p.parts or p.name=='manifest.json':continue
 manifest.append({'path':str(p.relative_to(R)),'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
(R/'manifest.json').write_text(json.dumps({'generated_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'excludes':'Git mirror object files are represented by verified history.bundle; manifest excludes itself. SQLite hash is inherited from original acquisition, not recomputed.','files':manifest},ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'public_files':len([p for p in OUT.rglob('*') if p.is_file()]),'local_manifest_files':len(manifest)}))
