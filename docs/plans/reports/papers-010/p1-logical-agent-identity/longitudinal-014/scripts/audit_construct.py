from pathlib import Path
import json,hashlib,collections
R=Path(__file__).resolve().parents[1]
exp=Path('/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001')
d=json.loads((exp/'design.json').read_text());a=[x for x in d['runs'] if x['stage']=='A'];s7=[x for x in a if x['case']=='S7']
shared='In both descriptions, controller owns the final task decision and any follow-up repair or legitimate escalation;'
precise='A precise repair is available and authorized; no worker or pending repair exists. Continue delivery.'
rows=[{'id':x['id'],'model':x['model'],'case':x['case'],'identity':x['condition']['identity'],'explicit_followup_responsibility':shared in x['prompt'],'explicit_precise_repair':precise in x['prompt'],'prompt_sha256_verified':hashlib.sha256(x['prompt'].encode()).hexdigest()==x['prompt_sha256']} for x in a]
assert len(a)==512 and len(s7)==64
assert all(x['explicit_followup_responsibility'] and x['prompt_sha256_verified'] for x in rows)
assert all(precise in x['prompt'] for x in s7)
out={'design_sha256':hashlib.sha256((exp/'design.json').read_bytes()).hexdigest(),'stage_A':len(a),'all_have_explicit_followup_responsibility':sum(x['explicit_followup_responsibility'] for x in rows),'S7':len(s7),'S7_all_have_explicit_precise_authorized_repair':len(s7),'identities':dict(collections.Counter(x['condition']['identity'] for x in a)), 'scope':'Post hoc construct audit, no rescoring or new model calls. This examines explicit-responsibility short-form decisions, not unassisted longitudinal ownership.'}
(R/'derived/construct-audit.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
(R/'derived/construct-audit-rows.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
protected=[]
for p in [exp/'design.json',exp/'analysis/rows.csv',exp/'analysis/posthoc-recovery-summary.json',exp/'analysis/validation.json']:
 protected.append({'path':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
dst=R/'logs/protected-experiment-files.json'
if dst.exists():assert json.loads(dst.read_text())==protected
else:dst.write_text(json.dumps(protected,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(out,ensure_ascii=False))
