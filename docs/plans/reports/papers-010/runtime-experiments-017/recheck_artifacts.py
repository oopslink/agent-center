"""Replay final acceptance on copies; never mutate primary episode artifacts."""
from pathlib import Path
import json,shutil,hashlib
from runtime import acceptance
ROOT=Path(__file__).parent
def main():
    dest=ROOT/'verification-replay';dest.mkdir(exist_ok=False);rows=[]
    for ep in sorted((ROOT/'episodes').iterdir()):
        p=ep/'result.json'
        if not p.exists():continue
        original=json.loads(p.read_text());row=original['row']
        if 'final_acceptance' not in original:rows.append({'id':row['id'],'status':'no_final_acceptance'});continue
        target=dest/row['id'];shutil.copytree(ep/'stop-state',target)
        result=acceptance(target,'replay-evaluator',row['task'],row['paper'],True)
        first=original['final_acceptance']
        agree=result['passed']==first['passed'] and result['errors']==first['errors'] and result['implementation']['candidate_hash']==first['implementation']['candidate_hash']
        if 'checks' in first['implementation']:agree=agree and result['implementation'].get('checks')==first['implementation']['checks']
        rows.append({'id':row['id'],'agreement':agree,'pass':result['passed'],'candidate_hash':result['implementation']['candidate_hash']})
    (ROOT/'analysis/artifact-replay.json').write_text(json.dumps({'scope':'Mechanical repeat on saved final snapshots; not new independent episodes or models','episodes':rows,'all_agree':all(x.get('agreement',False) for x in rows)},ensure_ascii=False,indent=2)+'\n')
    assert all(x.get('agreement',False) for x in rows)
    print(json.dumps({'replayed':len(rows),'all_agree':True}))
if __name__=='__main__':main()
