"""Verify the four contested receipts without changing any episode score."""
from pathlib import Path
import json,hashlib
ROOT=Path(__file__).parent
def main():
    ep=ROOT/'episodes/v2-P2-p2_expiry-haiku-long-distributed'
    receipts=[(p,json.loads(p.read_text())) for p in sorted((ep/'receipts').glob('*.json'))]
    events=[json.loads(l) for p in (ep/'events').glob('*.jsonl') for l in p.read_text().splitlines()]
    checks=[]
    for actor in ['worker-03','worker-04','worker-05','worker-06']:
        writes=[e for e in events if e['actor']==actor and e['kind']=='file_write' and e['value']['file']=='review.json']
        assert len(writes)==1
        event=writes[0];review=json.loads((ep/'versions'/event['value']['sha256']/'review.json').read_text())
        matches=[(p,x) for p,x in receipts if x['actor']==actor and x['candidate_hash']==review['candidate_hash'] and x['tests_hash']==review['tests_hash'] and x['passed'] and x['utc']<=event['utc']]
        assert len(matches)==1
        p,r=matches[0];assert r['utc'] in review['notes'] and actor in review['notes']
        checks.append({'actor':actor,'valid_prior_receipt':True,'source_sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
    result=json.loads((ep/'result.json').read_text())
    assert result['final_acceptance']['errors']==['manifest_missing_or_stale']
    output={'scope':'Post-result mechanical receipt audit, not a counterfactual model intervention or independent human review','episode':ep.name,'receipt_count':len(receipts),'review_checks':checks,'final_errors':result['final_acceptance']['errors']}
    (ROOT/'analysis/receipt-recompute.json').write_text(json.dumps(output,indent=2)+'\n');print(json.dumps(output))
if __name__=='__main__':main()
