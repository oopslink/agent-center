import unittest,tempfile,json,sys,subprocess,hashlib
from pathlib import Path
from unittest.mock import patch
import runtime as rt
import transport
from tasks import TASKS,vectors

class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.ep=Path(self.tmp.name)
        for d in ['work','events','versions','workers','receipts','calls','checks']:(self.ep/d).mkdir()
    def tearDown(self):self.tmp.cleanup()
    def candidate(self,task,reference=True):
        return rt.write(self.ep,'test-builder','builder','module.py',TASKS[task]['reference' if reference else 'seed'])
    def test_T01_all_seed_and_reference(self):
        for task in TASKS:
            with self.subTest(task=task):
                self.candidate(task,False);self.assertFalse(rt.check(self.ep,'fixture',task)['passed'])
                self.candidate(task);self.assertTrue(rt.check(self.ep,'fixture',task,True)['passed'])
    def test_T02_same_relation_facts(self):
        for organization in ['short','long']:
            base={'paper':'P2','organization':organization,'binding':'distributed'}
            split=rt.relation_view(base);joined=rt.relation_view({**base,'binding':'joined'})
            flat={r['role']:[x['file'] for x in r['permitted_outputs']] for r in joined['actor_relations']}
            self.assertEqual(flat,split['role_permissions'])
            for r in joined['actor_relations']:
                for f in r['permitted_outputs']:
                    self.assertEqual(f['requires'],split['artifact_dependencies'][f['file']]);self.assertEqual(f['contract'],split['artifact_contracts'][f['file']])
        for identity in ['unified','separate']:
            s=rt.system_prompt('controller',True,identity)
            self.assertNotIn('must repair',s);self.assertNotIn('responsibility',s);self.assertNotIn('next_action',s)
    def test_T03_permissions_and_immutable_versions(self):
        a=self.candidate('dev_abs');old=a['sha256']
        bad=rt.write(self.ep,'test-reviewer','reviewer','module.py','bad')
        self.assertFalse(bad['ok']);self.assertEqual(rt.sha(self.ep/'work/module.py'),old)
        self.assertFalse(rt.write(self.ep,'test-builder','builder','../escape','x')['ok'])
        self.candidate('dev_reverse');self.assertTrue((self.ep/'versions'/old/'module.py').exists())
    def test_T03_nested_tool_args_are_equivalent(self):
        flat={'op':'write_file','file':'module.py','content':TASKS['dev_abs']['reference']}
        nested={'op':'write_file','args':{'file':flat['file'],'content':flat['content']}}
        self.assertEqual(rt.normalized_action(flat),rt.normalized_action(nested))
        self.assertIsNone(rt.normalized_action({'op':'write_file','file':'a','args':{'file':'b'}}))
        self.assertEqual(rt.write(self.ep,'test-reviewer','reviewer',None,{})['error'],'missing_required_argument')
    def prepare_p2(self,receipt=True):
        task='dev_reverse';self.candidate(task)
        rt.write(self.ep,'test-builder','builder','test_cases.json',vectors(task))
        if receipt:rt.run_tests(self.ep,'test-verifier',task)
        fs=rt.files_state(self.ep)
        review={'candidate_hash':fs['module.py']['sha256'],'tests_hash':fs['test_cases.json']['sha256'],'verdict':'pass','notes':'Checked contract and tests.'}
        rt.write(self.ep,'test-verifier','verifier','review.json',review)
        fs=rt.files_state(self.ep)
        manifest={'candidate_hash':fs['module.py']['sha256'],'tests_hash':fs['test_cases.json']['sha256'],'review_hash':fs['review.json']['sha256']}
        rt.write(self.ep,'test-verifier','verifier','manifest.json',manifest)
        return task
    def test_T05_valid_release(self):
        task=self.prepare_p2();self.assertTrue(rt.acceptance(self.ep,'oracle',task,'P2',True)['passed'])
    def test_T05_retroactive_receipt_rejected(self):
        task=self.prepare_p2(False)
        self.assertIn('review_missing_stale_or_unverified',rt.acceptance(self.ep,'oracle',task,'P2',True)['errors'])
        rt.run_tests(self.ep,'test-verifier',task)
        self.assertIn('review_missing_stale_or_unverified',rt.acceptance(self.ep,'oracle',task,'P2',True)['errors'])
    def test_T05_stale_artifacts(self):
        task=self.prepare_p2();self.candidate('dev_abs')
        out=rt.acceptance(self.ep,'oracle',task,'P2',True)
        self.assertIn('review_missing_stale_or_unverified',out['errors']);self.assertIn('manifest_missing_or_stale',out['errors'])
    def test_T05_bad_test_expectation(self):
        self.candidate('dev_reverse');cases=vectors('dev_reverse');cases[0]['output']=[99]
        rt.write(self.ep,'test-builder','builder','test_cases.json',cases)
        self.assertEqual(rt.run_tests(self.ep,'tester','dev_reverse')['user_tests']['error'],'test_contract_invalid')
    def test_T06_transport_retry_and_format(self):
        self.assertIsNone(transport.parse('not json'));self.assertEqual(transport.parse('```json\n{"op":"wait"}\n```'),{'op':'wait'})
        calls=[subprocess.CompletedProcess([],1,'','temporary error'),subprocess.CompletedProcess([],0,json.dumps({'type':'result','subtype':'success','is_error':False,'result':'{"op":"finish"}','modelUsage':{'fixed':{}},'total_cost_usd':0}),'')]
        with patch.object(transport.subprocess,'run',side_effect=calls):
            out=transport.invoke('fake','system',[],self.ep/'retry')
        self.assertTrue(out['transport_ok']);self.assertEqual(out['attempt'],2)
        self.assertTrue((self.ep/'retry/attempt-1/metadata.json').exists())
    def test_T04_real_worker_process_mock_model(self):
        self.candidate('dev_abs',False)
        request={'worker_id':'worker-01','role':'executor','task':'Implement the contract.','row':{'model':'fake','task':'dev_abs','identity':'unified'}}
        p=self.ep/'workers/request.json';rt.save(p,request)
        actions=[{'op':'write_file','file':'module.py','content':TASKS['dev_abs']['reference']},{'op':'run_tests'},{'op':'finish','status':'complete','message':'Implemented and checked.'}]
        script="import runtime,json; from pathlib import Path\nactions=iter("+repr(actions)+")\ndef fake(*a,**k):\n x=next(actions);return {'transport_ok':True,'parsed':x,'text':json.dumps(x)}\nruntime.invoke=fake\nruntime.worker(Path("+repr(str(self.ep))+"),"+repr(str(p))+")\n"
        process=subprocess.run([sys.executable,'-c',script],cwd=rt.ROOT,capture_output=True,text=True)
        self.assertEqual(process.returncode,0,process.stderr)
        result=json.loads((self.ep/'workers/worker-01-result.json').read_text())
        self.assertEqual(result['status'],'reported_complete');self.assertNotEqual(result['pid'],__import__('os').getpid())
        self.assertTrue(rt.check(self.ep,'checker','dev_abs',True)['passed'])
    def test_T07_early_finish_and_idle_actions(self):
        temp=self.ep/'trajectory'
        actions=iter([{'op':'wait'},{'op':'finish','status':'complete','message':'Done.'}])
        def fake(*args,**kwargs):
            x=next(actions);return {'transport_ok':True,'parsed':x,'text':json.dumps(x)}
        with patch.object(rt,'invoke',side_effect=fake):
            out=rt.episode({'id':'mock','paper':'P1','task':'dev_abs','model':'fake','identity':'separate','ledger':'off'},temp)
        self.assertTrue(out['premature_completion']);self.assertFalse(out['autonomous_delivery']);self.assertEqual(out['no_active_no_artifact_change_actions'],1)
    def test_private_vectors_not_public_feedback(self):
        for task,v in TASKS.items():
            if v['split']=='main':
                self.candidate(task)
                out=rt.check(self.ep,'checker',task)
                self.assertEqual(out['total'],3);self.assertGreaterEqual(len(vectors(task))-out['total'],5)

if __name__=='__main__':unittest.main(verbosity=2)
