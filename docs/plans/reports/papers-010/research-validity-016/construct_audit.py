# -*- coding: utf-8 -*-
"""Read-only construct audit of all frozen P1-P4 primary action inputs.

No model calls, rescoring, or changes to the original experimental artifacts.
"""
from pathlib import Path
import csv, json, hashlib, collections, importlib.util

R=Path(__file__).resolve().parent
E=Path('/Users/oopslink/works/research/agent-harness-study/run-001/experiments')
W=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers/docs/plans/reports/papers-010')
sources=[E/'p1-identity-001/design.json',E/'p234-mechanisms-002/design.json',E/'p234-mechanisms-002/design_lib.py']
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
source_hashes=[{'path':str(p),'sha256':sha(p)} for p in sources]
designs=[json.loads(p.read_text()) for p in sources[:2]]
phrases={
 'P1':{'shared_repair_obligation':'In both descriptions, controller owns the final task decision and any follow-up repair or legitimate escalation;',
       'prespecified_authorized_repair':'A precise repair is available and authorized; no worker or pending repair exists. Continue delivery.'},
 'P2':{'human_preparation_instruction':'Do not ask a human to do authorized preparation.',
       'gate_not_tool_step':'gates are predicates, not mandatory tool calls.',
       'same_relation_instruction':'Binding views and separate tables express the same relations.'},
 'P3':{'name_permission_separation':'Display names identify agents but do not grant permission or change stable IDs.',
       'explicit_disposition_rule':'A current failed check is enough to reject. If the check is unknown, request_context.',
       'current_scope_instruction':'Records from other projects/contracts/versions are not evidence for the target.'},
 'P4':{'graph_artifact_scope_separation':'graph history, artifact content and acceptance scope are separate.',
       'restore_rule':'If a candidate lost a required retained feature, restore the retaining predecessor as repair base.',
       'history_dependency_declared_absent':'Closed graph history has no current execution dependency.',
       'graph_does_not_repair_code':'no operation repairs code merely by editing a graph.'}}
rows=[]
for idx,d in enumerate(designs):
 for run in d['runs']:
  if idx==0 and run['stage']!='A':continue
  paper='P1' if idx==0 else run['paper'];prompt=run['prompt']
  actual=hashlib.sha256(prompt.encode()).hexdigest();assert actual==run['prompt_sha256'],run['id']
  row={'paper':paper,'id':run['id'],'case':run['case'],'model':run['model'],'prompt_sha256':actual,'hash_verified':True}
  row.update({key:phrase in prompt for key,phrase in phrases[paper].items()});rows.append(row)
fields=['paper','id','case','model','prompt_sha256','hash_verified']+list(dict.fromkeys(k for v in phrases.values() for k in v))
with (R/'construct-input-rows.csv').open('w') as f:
 writer=csv.DictWriter(f,fieldnames=fields);writer.writeheader();writer.writerows(rows)
summary={}
for paper,keys in phrases.items():
 rs=[r for r in rows if r['paper']==paper]
 summary[paper]={'action_inputs':len(rs),'unique_scenarios':len(set(r['case'] for r in rs)),
  'counts':{key:sum(bool(r[key]) for r in rs) for key in keys},
  'phrases':keys}
assert summary['P1']['action_inputs']==512 and summary['P1']['counts']['shared_repair_obligation']==512
assert summary['P1']['counts']['prespecified_authorized_repair']==64
for paper in ['P2','P3','P4']:
 assert summary[paper]['action_inputs']==256
 assert all(n==256 for n in summary[paper]['counts'].values())
# Recompute constructed P3 coverage using the frozen deterministic selection/truth code.
spec=importlib.util.spec_from_file_location('frozen_design_lib',sources[2]);mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
coverage=[]
for case in mod.p3_cases():
 for policy in ['personal','role','all','bounded']:
  truth=mod.p3_truth(case,policy)
  coverage.append({'case':case['id'],'policy':policy,'record_count':truth['context_records'],'full_coverage':truth['full_coverage'],'sufficient':truth['sufficient'],'decision':truth['decision']})
with (R/'p3-constructed-coverage.csv').open('w') as f:
 writer=csv.DictWriter(f,fieldnames=list(coverage[0]));writer.writeheader();writer.writerows(coverage)
coverage_summary={policy:{'scenarios':8,'full_coverage':sum(r['full_coverage'] for r in coverage if r['policy']==policy),'sufficient':sum(r['sufficient'] for r in coverage if r['policy']==policy)} for policy in ['personal','role','all','bounded']}
assert coverage_summary['personal']['sufficient']==3 and coverage_summary['role']['sufficient']==2
assert coverage_summary['all']['sufficient']==coverage_summary['bounded']['sufficient']==8
# Reconcile original immutable CSV counts, separately from the later diagnostic scoring.
csv_counts={}
for p in sorted(W.glob('p[1-4]-*/experiment-results.csv')):
 rs=list(csv.DictReader(p.open()));paper=p.parent.name[:2].upper()
 if paper=='P1':rs=[r for r in rs if r['stage']=='A']
 models={}
 for model in sorted(set(r['model'] for r in rs)):
  mr=[r for r in rs if r['model']==model];key='progress' if paper=='P1' else 'correct'
  models[model]={'rows':len(mr),'original_primary_match':sum(r[key]=='True' for r in mr),'format_invalid':sum(r['format_valid']!='True' for r in mr)}
 csv_counts[paper]={'rows':len(rs),'models':models,'path':str(p),'sha256':sha(p)}
audit={'scope':'Frozen primary-input construct census; no new model calls or rescoring. Explicit rules are not evidence of training-data leakage.',
 'sources':source_hashes,'input_census':summary,'total_primary_action_inputs':len(rows),'all_prompt_hashes_verified':True,
 'P3_constructed_coverage':coverage_summary,'original_csv_reconciliation':csv_counts,
 'posthoc_invalid_scenarios':{'P2':{'cases':['W2','W8'],'scenario_denominator':8,'excluded_scenarios':2,'scenario_fraction':0.25,'all_condition_outputs':64},'P4':{'cases':['D5','D8'],'scenario_denominator':8,'excluded_scenarios':2,'scenario_fraction':0.25,'all_condition_outputs':64}},
 'interpretation':'Protocols directly state several principles later discussed in conclusions. Results can measure adherence and local framing sensitivity conditional on those rules, not independently establish their operational necessity, architecture benefit, long-run liveness, or training origin. P3 coverage is constructed by deterministic selection, not learned retrieval performance.'}
(R/'construct-audit.json').write_text(json.dumps(audit,ensure_ascii=False,indent=2)+'\n')
assert all(sha(Path(s['path']))==s['sha256'] for s in source_hashes)
print(json.dumps({'verified_inputs':len(rows),'P1':summary['P1']['action_inputs'],'P2':summary['P2']['action_inputs'],'P3':summary['P3']['action_inputs'],'P4':summary['P4']['action_inputs'],'coverage':coverage_summary},ensure_ascii=False))
