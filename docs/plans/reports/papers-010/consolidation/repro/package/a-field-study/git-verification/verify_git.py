from pathlib import Path
import json,subprocess,os
a=Path(__file__).resolve().parent
root=Path(os.environ.get('STUDY_ROOT','<ARCHIVE>/works/research/agent-harness-study/run-001'))
orig=json.loads((root/'evolution-008/logs/source-checks.json').read_text())
out=[]
for v in orig:
 if not v.get('check'):continue
 cmd=v['command'];assert cmd[0]=='git' and cmd[1] in ['merge-base','show','ls-tree']
 u=subprocess.run(cmd,capture_output=True,text=True,cwd=v['cwd'])
 ok=(u.returncode==v['exit_code'] and u.stdout.strip()==v['stdout'].strip())
 out.append({'check':v['check'],'command':cmd,'rc':u.returncode,'stdout':u.stdout.strip(),'matches_previous':ok})
(a/'git-replay.json').write_text(json.dumps(out,indent=2)+'\n')
print([(i['check'],i['matches_previous']) for i in out])
