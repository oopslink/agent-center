"""Capture code provenance; artifact presence is not proof of activation."""
from pathlib import Path
import subprocess,json,hashlib
R=Path(__file__).resolve().parents[1]
REPO=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
def run(*args):return subprocess.run(['git','-C',str(REPO),*args],capture_output=True)
def dump(p,v):
 p=R/p;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
versions=['9e09f2a2064e155c4b673b63a270f92130b543c1','71cde362622df2a28c156e7abfa8d8906a0aeebb','48892d61efdf17c69e8c317b64c5bda2c5b18fc2']
paths=['internal/admin/api/agent_tools_team.go','internal/cognition/ruleregistry/sqlite/audit_repo.go',
 'internal/persistence/migrations/0128_team_rule_load_audits.up.sql','internal/mcphost/planning_rules.go',
 'internal/cognition/memory/runtime.go','internal/cognition/memory/skeleton.go','internal/agentruntime/memory_sync.go',
 'internal/cognition/memory/centergit/team_memory_repository.go','internal/cognition/memory/centergit/team_memory_service.go']
rows=[]
for v in versions:
 for p in paths:
  x=run('show',v+':'+p); out=R/'raw/source'/v[:8]/p
  if x.returncode==0:out.parent.mkdir(parents=True,exist_ok=True);out.write_bytes(x.stdout)
  rows.append({'commit':v,'path':p,'exists':x.returncode==0,'sha256':hashlib.sha256(x.stdout).hexdigest() if x.returncode==0 else None,'bytes':len(x.stdout),'error':x.stderr.decode() if x.returncode else ''})
dump('derived/source-provenance.json',rows)
hist=run('log','--all','--format=%H%x09%aI%x09%cI%x09%s','--',*paths)
(R/'raw/source-history.tsv').write_bytes(hist.stdout)
milestones=[]
for v in ['4e588233','d80eb55e','c5ba6eed','3a06de58','332ca77f','50f2812b','bacc8eaf','fe5e05c6','61cebc7b','4f0e990c','38172447']:
 x=run('show','-s','--format=%H%x09%aI%x09%cI%x09%s',v)
 milestones.append({'query':v,'exit':x.returncode,'git_metadata':x.stdout.decode().strip()})
dump('derived/source-milestones.json',milestones)
artifacts=json.loads((R/'derived/deployment-files.json').read_text())
for x in artifacts:
 v=x['commit'];p=run('rev-parse','--verify',v+'^{commit}');x['local_git_resolvable']=p.returncode==0
 x['resolved_source_commit']=p.stdout.decode().strip() if p.returncode==0 else None
 x['evidence_scope']='current symlink only' if 'current_symlink' in x else 'artifact retained; activation time unknown'
dump('derived/deployment-source-matrix.json',artifacts)
print(json.dumps({'source_files':len(rows),'captured':sum(r['exists'] for r in rows),'artifact_rows':len(artifacts),'resolvable':sum(x['local_git_resolvable'] for x in artifacts)}))
