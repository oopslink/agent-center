"""Rebuild descriptive results from immutable local memory/DB captures; no LLM calls."""
from pathlib import Path
import collections, csv, datetime as dt, hashlib, json, statistics, subprocess, re
import yaml

R = Path(__file__).resolve().parents[1]
CUTOFF = '2026-11-02T06:41:08.656823Z'
def load(p): return json.loads((R/p).read_text())
def dump(p,v):
    p=R/p; p.parent.mkdir(parents=True,exist_ok=True); p.write_text(json.dumps(v,ensure_ascii=False,indent=2,default=str)+'\n')
def csvout(p,rows):
    if not rows:return
    with (R/p).open('w') as f:
        w=csv.DictWriter(f,fieldnames=list(rows[0]));w.writeheader();w.writerows(rows)
def time(x):
    x=x.replace('Z','+00:00')
    x=re.sub(r'\.(\d+)(?=[+-]\d\d:\d\d$)',lambda m:'.'+m[1][:6].ljust(6,'0'),x)
    return dt.datetime.fromisoformat(x)
def git(repo,*args):
    return subprocess.check_output(['git','--git-dir',str(R/'raw/memory'/repo/'repo.git'),*args])
def metadata(b):
    s=b.decode(); return yaml.safe_load(s.split('---',2)[1]) if s.startswith('---') else {}
def stats(xs):
    return {'n':len(xs),'min':min(xs),'median':statistics.median(xs),'max':max(xs)} if xs else {'n':0}

docs=load('derived/head-documents.json'); audits=load('raw/db/team-rule-load-audits.json')
events=load('raw/db/memory_events-corrected.json')
for e in events:
    e['decoded']=json.loads(e['payload'])
props=[];checks=[]
for d in docs:
    if not d['path'].startswith('proposals/'):continue
    f=d['frontmatter'];review=f.get('reviewed_at'); created=f['created_at']
    props.append({'team_id':d['repo'][5:],'proposal_id':f['proposal_id'],'operation':f['operation'],
      'kind':f['target_kind'],'status':f['status'],'created_at':created,'reviewed_at':review or '',
      'reviewer_type':f.get('reviewer_ref','').split(':')[0],
      'author_equals_reviewer':f.get('author_ref')==f.get('reviewer_ref'),
      'review_minutes':(time(review)-time(created)).total_seconds()/60 if review else '',
      'pending_days_at_cutoff':(time(CUTOFF)-time(created)).total_seconds()/86400 if not review else '',
      'source_path':f.get('source_path','')})
    for e in events:
        p=e['decoded']
        if p['proposal_id']!=f['proposal_id']:continue
        sha=p['repo_commit']; historical=metadata(git(d['repo'],'show',sha+':'+d['path']))
        checks.append({'event_id':e['id'],'event_type':e['event_type'],'team_id':p['team_id'],
            'proposal_id':p['proposal_id'],'repo_commit':sha,'git_proposal_status':historical['status'],
            'event_status':p['status'],'status_matches':historical['status']==p['status'],
            'event_occurred_at':e['occurred_at'],'event_persisted_at':e['created_at'],
            'git_commit_time':git(d['repo'],'show','-s','--format=%cI',sha).decode().strip()})
git_commits={git(d['repo'],'rev-parse',c).decode().strip() for d in docs if d['path']=='MEMORY.md' for c in git(d['repo'],'rev-list','--all').decode().splitlines()}
csvout('derived/proposals.csv',props);dump('derived/event-git-validation.json',checks)
trees={}; enriched=[]
for a in audits:
    repo='team-'+a['team_id'];key=(repo,a['team_memory_commit'])
    if key not in trees:
        trees[key]={}
        for p in git(repo,'ls-tree','-r','--name-only',key[1],'rules').decode().splitlines():
            b=git(repo,'show',key[1]+':'+p); m=metadata(b)
            trees[key][m['name']]={'path':p,'meta':m,'blob':git(repo,'rev-parse',key[1]+':'+p).decode().strip(),'bytes':len(b)}
    item=trees[key].get(a['rule_slug'])
    prior=[e for e in events if e['decoded']['team_id']==a['team_id'] and time(e['created_at'])<=time(a['loaded_at'])]
    latest=max(prior,key=lambda e:time(e['created_at'])) if prior else None
    latest_sha=latest['decoded']['repo_commit'] if latest else ''
    latest_blob=git(repo,'rev-parse',latest_sha+':'+item['path']).decode().strip() if latest and item else ''
    enriched.append({**a,'resolved':bool(item),'enabled':item['meta'].get('enabled') if item else False,
       'phase_allowed':a['phase'] in item['meta'].get('applies_to',[]) if item else False,
       'rule_path':item['path'] if item else '', 'rule_blob':item['blob'] if item else '',
       'latest_event_commit':latest_sha,'different_from_latest_event_commit':latest_sha!=a['team_memory_commit'],
       'same_rule_blob_as_latest_event':bool(item) and item['blob']==latest_blob,
       'context_type':'execution' if a['execution_id'] else 'supervisor_runtime_generation'})
csvout('derived/read-receipts.csv',enriched)
rules=[]
for d in docs:
    if not d['path'].startswith('rules/'):continue
    f=d['frontmatter']; team=d['repo'][5:]; rows=[a for a in enriched if a['team_id']==team and a['rule_slug']==f['name']]
    p=next(p for p in props if p['team_id']==team and p['source_path']==d['path'])
    first=min([a['loaded_at'] for a in rows],default='')
    rules.append({'team_id':team,'rule_slug':f['name'],'bytes_at_head':d['bytes'],'promoted_at':p['reviewed_at'],
       'first_audit_at':first,'hours_promotion_to_first_audit':(time(first)-time(p['reviewed_at'])).total_seconds()/3600 if first else '',
       'receipt_count':len(rows),'agents':len(set(a['agent_id'] for a in rows)),
       'contexts':len(set(a['execution_id'] or a['planning_session_id'] for a in rows))})
csvout('derived/rules.csv',rules)
matrix=[]
agents={a['id']:a['name'] for a in load('raw/db/agents.json')}
for (agent,slug),n in sorted(collections.Counter((a['agent_id'],a['rule_slug']) for a in enriched).items()):
    matrix.append({'agent_id':agent,'agent_name':agents[agent],'rule_slug':slug,'receipts':n})
csvout('derived/agent-rule-matrix.csv',matrix)
inv=load('derived/repo-inventory.json'); scopes=[]
for x in inv:
    for f in x['worktree_files']:
        parts=Path(f['path']).parts
        scope='task' if 'tasks' in parts else 'issue' if 'issues' in parts else 'project' if 'projects' in parts else 'agent_root' if x['label'].startswith('agent-') else 'legacy_root'
        scopes.append({'repo':x['label'],'path':f['path'],'scope':scope,'bytes':f['bytes'],'sha256':f['sha256']})
csvout('derived/private-file-inventory.csv',scopes)
reviewed=[p for p in props if p['reviewed_at']]
summary={
 'cutoff':CUTOFF,'proposals':len(props),'proposal_status':dict(collections.Counter(p['status'] for p in props)),
 'operations':dict(collections.Counter(p['operation'] for p in props)),
 'reviewer_type':dict(collections.Counter(p['reviewer_type'] for p in reviewed)),
 'self_reviewed':sum(p['author_equals_reviewer'] for p in reviewed),
 'review_minutes_all':stats([p['review_minutes'] for p in reviewed]),
 'review_minutes_by_team':{t:stats([p['review_minutes'] for p in reviewed if p['team_id']==t]) for t in sorted(set(p['team_id'] for p in props))},
 'pending_days':[{k:p[k] for k in ['proposal_id','pending_days_at_cutoff']} for p in props if not p['reviewed_at']],
 'canonical_rules':len(rules),'canonical_entries':sum(d['path'].startswith('entries/') for d in docs),
 'events':len(events),'team_git_commits':len(git_commits),'events_unique_commits':len(set(e['decoded']['repo_commit'] for e in events)),
 'event_commit_set_equals_git':{e['decoded']['repo_commit'] for e in events}==git_commits,
 'event_status_matches':sum(c['status_matches'] for c in checks),
 'read_receipts':len(audits),'read_first':min(a['loaded_at'] for a in audits),'read_last':max(a['loaded_at'] for a in audits),
 'receipt_agents':len(set(a['agent_id'] for a in audits)),
 'receipt_contexts':len(set(a['execution_id'] or a['planning_session_id'] for a in audits)),
 'supervisor_contexts':len(set(a['planning_session_id'] for a in audits if a['planning_session_id'])),
 'execution_contexts':len(set(a['execution_id'] for a in audits if a['execution_id'])),
 'receipts_by_context_type':dict(collections.Counter(a['context_type'] for a in enriched)),
 'receipts_by_phase_first_only':dict(collections.Counter(a['phase'] for a in audits)),
 'rules_with_audit':sum(r['receipt_count']>0 for r in rules),'rules_read_by_multiple_agents':sum(r['agents']>1 for r in rules),
 'audit_rule_resolves':sum(a['resolved'] for a in enriched),'audit_rule_enabled':sum(bool(a['enabled']) for a in enriched),
 'audit_phase_allowed':sum(a['phase_allowed'] for a in enriched),
 'older_repo_commit_receipts':sum(a['different_from_latest_event_commit'] for a in enriched),
 'same_rule_blob_as_latest_receipts':sum(a['same_rule_blob_as_latest_event'] for a in enriched),
 'private_files_by_scope':{s:{'files':sum(x['scope']==s for x in scopes),'bytes':sum(x['bytes'] for x in scopes if x['scope']==s)} for s in sorted(set(x['scope'] for x in scopes))},
 'legacy_files_by_scope':{s:{'files':sum(x['scope']==s and x['repo']=='worker-legacy' for x in scopes),'bytes':sum(x['bytes'] for x in scopes if x['scope']==s and x['repo']=='worker-legacy')} for s in sorted(set(x['scope'] for x in scopes if x['repo']=='worker-legacy'))}
}
dump('derived/summary.json',summary)
assert summary['event_commit_set_equals_git'] and summary['event_status_matches']==57
assert summary['audit_rule_resolves']==summary['audit_rule_enabled']==summary['audit_phase_allowed']==230
print(json.dumps(summary,ensure_ascii=False,indent=2))
