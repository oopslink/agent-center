# -*- coding: utf-8 -*-
import unittest,tempfile,shutil
from pathlib import Path
from runtime import *
class Audit(unittest.TestCase):
 def test_contracts_and_boundary(self):
  for row in [r for r in DESIGN if r['model']==MODELS['S'] and r['rep']==1]:
   with self.subTest(row=row['id']),tempfile.TemporaryDirectory() as tmp:
    w=Path(tmp)/'a';init(w,row);self.assertFalse(action(w,'accept','')['accepted_checks'])
    first=action(w,'dispatch','');s=load(w/'state.json')
    if row['access']:
     self.assertFalse(first['accepted_checks']);self.assertTrue(s['candidate'])
     self.assertEqual(first['missing'],['counts mismatch: api=2, web=1 required'] if row['case']=='D' else ['candidate does not contain required base'])
     self.assertTrue(action(w,'dispatch','')['accepted_checks']);self.assertTrue(action(w,'accept','')['accepted_checks']);self.assertTrue(load(w/'state.json')['accepted'])
    else:self.assertIn('permission_denied',first['result']);self.assertEqual(s['launches'],0);self.assertIsNone(s['candidate'])
 def test_fork_isolation(self):
  with tempfile.TemporaryDirectory() as tmp:
   d=Path(tmp);src=d/'source';init(src,DESIGN[0]);action(src,'dispatch','')
   for a in ARMS:
    w=d/a;shutil.copytree(src,w);self.assertEqual(inspect(w),inspect(src))
    treatment(w,a); self.assertEqual(bool(load(w/'state.json')['returned_to']),a=='R')
    action(w,'dispatch','');self.assertFalse(inspect(src)['accepted_checks'])
 def test_no_target_in_opening(self):
  with tempfile.TemporaryDirectory() as tmp:
   w=Path(tmp)/'w';init(w,DESIGN[0]);s=str(starter(DESIGN[0],w))
   for forbidden in ('你的一部分','身份错位','我只是','后续仍由你','goal_responsibility_returned'):self.assertNotIn(forbidden,s)
if __name__=='__main__':unittest.main()
