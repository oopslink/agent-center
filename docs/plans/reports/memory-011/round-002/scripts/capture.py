# -*- coding: utf-8 -*-
import json,subprocess,hashlib,datetime,sqlite3,re,os
from pathlib import Path
R=Path(__file__).resolve().parents[1]
AC=Path('/Users/oopslink/.agent-center')
WORKER=AC/'workers/worker-edb09a0c'
REPO=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
def dump(p,v):
 p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
def run(args,cwd=None):
 p=subprocess.run(args,cwd=cwd,text=True,capture_output=True)
 return {'exit':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
def git(p,*args):return run(['git','-C',str(p),*args])
def digest(b):return hashlib.sha256(b).hexdigest()
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
repos=[]
for p in sorted((AC/'var/team-git').rglob('HEAD')):
 if p.parent.name.endswith('.git'):repos.append((('team-' if p.parent.parent.name=='team' else '')+p.parent.stem,p.parent))
for p in sorted((WORKER/'var/agents').glob('*/memory')):repos.append(('agent-'+p.parent.name,p))
repos.append(('worker-legacy',WORKER/'memory'))
inventory=[]
for label,p in repos:
 out=R/'raw/memory'/label;out.mkdir(parents=True,exist_ok=True)
 refs=git(p,'show-ref');valid=git(p,'rev-parse','--git-dir')['exit']==0
 row={'label':label,'source':str(p),'capture_utc':utc(),'git_repo':valid,'refs_before':refs,'worktree_files':[]}
 if valid:
  row['head']=git(p,'rev-parse','HEAD');row['status']=git(p,'status','--porcelain=v1')
  row['count']=git(p,'rev-list','--all','--count')
  bundle=git(p,'bundle','create',str(out/'history.bundle'),'--all');row['bundle_create']=bundle
  if bundle['exit']==0:
   row['bundle_verify']=git(p,'bundle','verify',str(out/'history.bundle'))
   row['clone']=run(['git','clone','--mirror',str(out/'history.bundle'),str(out/'repo.git')])
   row['bundle_sha256']=digest((out/'history.bundle').read_bytes())
   log=git(out/'repo.git','log','--all','--format=%H%x09%P%x09%aI%x09%cI%x09%an%x09%s')
   (out/'commits.tsv').write_text(log['stdout']);row['log_exit']=log['exit']
   tree=git(out/'repo.git','ls-tree','-r','--long','HEAD');(out/'head-tree.txt').write_text(tree['stdout'])
 if not p.name.endswith('.git'):
  for f in sorted(p.rglob('*')):
   if '.git' in f.relative_to(p).parts or not f.is_file() or f.is_symlink():continue
   b=f.read_bytes();rel=f.relative_to(p);dst=out/'working-tree'/rel;dst.parent.mkdir(parents=True,exist_ok=True);dst.write_bytes(b)
   row['worktree_files'].append({'path':str(rel),'bytes':len(b),'sha256':digest(b),'mtime_ns':f.stat().st_mtime_ns,'stable_after_copy':digest(f.read_bytes())==digest(b)})
 row['refs_after']=git(p,'show-ref');row['refs_stable']=row['refs_before']==row['refs_after']
 inventory.append(row)
dump(R/'derived/repo-inventory.json',inventory)
versions=[]
for side,root in [('center',AC),('worker',WORKER)]:
 for p in sorted((root/'versions').rglob('COMMIT')):
  parent=p.parent;ver=parent/'VERSION'
  versions.append({'side':side,'directory':str(parent.relative_to(root)),'commit':p.read_text().strip(),'version':ver.read_text().strip() if ver.exists() else None,'commit_file_mtime_utc':datetime.datetime.fromtimestamp(p.stat().st_mtime,datetime.timezone.utc).isoformat()})
 current=(root/'current').resolve(); versions.append({'side':side,'current_symlink':str(current),'commit':(current/'COMMIT').read_text().strip(),'version':(current/'VERSION').read_text().strip(),'observed_utc':utc()})
dump(R/'derived/deployment-files.json',versions)
# Bounded append-only log reads, recording original offsets and hashes.
log_paths=list((AC/'logs').glob('*.log'))+[AC/'var/center-redeploy.log']+list((WORKER/'logs').glob('*.log'))+list((WORKER/'var').glob('*.log'))
patterns=re.compile(rb'memory|MEMORY|[Vv]ersion[= :]|[Cc]ommit[= :]|[Ss]tart(?:ing|ed)|snapshot|deplo|release|rule.{0,40}(?:index|commit)',re.I)
logs=[]
for idx,p in enumerate(log_paths):
 size=p.stat().st_size;sha=hashlib.sha256();n=0;offset=0;rows=0
 with p.open('rb') as f,(R/'raw'/('log-%02d.jsonl'%idx)).open('w') as out:
  while offset<size:
   line=f.readline(size-offset)
   if not line:break
   sha.update(line);n+=1
   if patterns.search(line):
    out.write(json.dumps({'line':n,'offset':offset,'text':line.decode('utf8','replace').rstrip('\n')},ensure_ascii=False)+'\n');rows+=1
   offset+=len(line)
 logs.append({'id':'log-%02d'%idx,'source':str(p),'bytes_at_start':size,'bytes_read':offset,'sha256_of_read_prefix':sha.hexdigest(),'matched_lines':rows,'lines_scanned':n,'bytes_after':p.stat().st_size,'pattern':patterns.pattern.decode()})
dump(R/'derived/log-inventory.json',logs)
# Existing DB snapshot. Never read agent.env_vars or token tables.
db=Path('/Users/oopslink/works/research/agent-harness-study/run-001/raw/snapshot.sqlite')
c=sqlite3.connect(db.as_uri()+'?mode=ro');c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON')
queries={
 'agents':'SELECT id,name,identity_member_id,worker_id,lifecycle,created_at,updated_at FROM agents',
 'teams':'SELECT * FROM teams',
 'members':'SELECT * FROM team_members',
 'policies':'SELECT * FROM team_memory_policies',
 'curators':'SELECT * FROM team_memory_policy_curators',
 'workers':'SELECT id,name,status,last_heartbeat_at,updated_at,system_info_json FROM workers',
 'activity_coverage':'SELECT count(*) n,min(occurred_at) first,max(occurred_at) last FROM agent_activity_events',
 'activity_by_type':'SELECT event_type,count(*) n FROM agent_activity_events GROUP BY event_type',
 'memory_events':"SELECT * FROM events WHERE event_type LIKE 'team.memory.%' OR event_type LIKE 'memory.%'",
}
ql=[]
for name,sql in queries.items():
 started=utc();rows=[dict(x) for x in c.execute(sql)];dump(R/'raw/db'/f'{name}.json',rows);ql.append({'name':name,'sql':sql,'started_utc':started,'rows':len(rows),'finished_utc':utc()})
c.close();dump(R/'logs/queries.json',ql)
dump(R/'logs/capture-complete.json',{'finished_utc':utc(),'repos':len(repos),'versions':len(versions),'logs':len(logs)})
print(json.dumps({'repos':[{'label':x['label'],'git':x['git_repo'],'commits':x.get('count',{}).get('stdout','').strip(),'working_files':len(x['worktree_files'])} for x in inventory],'versions':len(versions),'logs':[{'id':x['id'],'matched':x['matched_lines']} for x in logs],'db_queries':[{'name':x['name'],'rows':x['rows']} for x in ql]},ensure_ascii=False))
