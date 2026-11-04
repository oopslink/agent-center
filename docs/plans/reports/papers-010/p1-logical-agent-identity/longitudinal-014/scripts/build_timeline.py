"""Reconstruct selected correction chains; preserve all matches and counterevidence."""
from pathlib import Path
import sqlite3,json,hashlib,datetime as dt,re
R=Path(__file__).resolve().parents[1]
c=sqlite3.connect('file:/Users/oopslink/works/research/agent-harness-study/run-001/raw/snapshot.sqlite?mode=ro',uri=True);c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON')
def dump(p,x):
 p=R/p;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
cases=[
 {'id':'L01','type':'mixed-trigger/ownership-correction','anchors':['01KY4B5KS3272X3AMXDXHQMEC0','01KY4B74V6XGRFG85EV29RS1RT','01KY4B7K5QXK434NNHY9K1FKEF'],'limit':'线程故障排除；只保留内部失败被当外部责任的表述与纠正，不证明合法恢复全部可行。'},
 {'id':'L02','type':'mixed-trigger/ownership-correction','anchors':['01KY5BMKX9ZM03BTJTENTQSKDB','01KY5BYKS2T49YEXAGRPQQ9XWN','01KY5BZ3804ZA0ETH34RA1XEH1','01KY5C4XXCTE2AGYB373M42A9Y','01KY5C5HW683A4YZG91P23RC7G'],'limit':'空workspace/materializer混杂；不把仓库缺失或整个阻塞结果归于身份封装。'},
 {'id':'L03','type':'contemporaneous-operator-observation-and-design-response','anchors':['01KY68C1K5SHCFA4SFNZSS7C4C','01KY68TD24Y07FCY5CZY56JEDZ','01KY699NV8C4KRZ8DNXC4858KM','01KY69AW978C5TVS6GBM5TXK6N'],'limit':'所有agent是操作者当时观察，不是全体逐例标注；源码改动不证明当日已部署。'},
 {'id':'L04','type':'startup-responsibility-correction','anchors':['01KZABVT594GFNYTWZ7AZRHF30','01KZABY7HSVA1FF42EH7XYX7GX','01KZAC08EKE041V1XB1C2XWE5V','01KZACFHV3BFM5CP04CR07Q79W'],'limit':'reset说明缺陷与派发变更仍混杂；创建执行体仅有报告，不重分类成架构净因果案例。'},
 {'id':'L05','type':'positive-responsibility-and-quality-review','anchors':['01KZDJ41E4XMR6RSRDW2342DCE','01KZDKN7K7XRNZ0DPE7G9V0GQA','01KZDKS2M645VTZJ9GR8SW0MDJ'],'limit':'正确承责可见；不是无人工干预基线，不能否定早期困难或证明团队永久改善。'},
 {'id':'L06','type':'visibility-complaint/not-repair-failure','anchors':['01KZG850KPD719RY7K3PVMXMFN'],'limit':'身份提醒嵌在进度不可见投诉，不单独计作修复责任中断。'},
 {'id':'L07','type':'mixed-runtime/ownership-wording','anchors':['01KZQQMJHHZ2BR69ZVVA6Z2ATQ','01KZQQTJTZHXSQ687330E7GJE8','01KZQQVEC2Y51A0ACEVK57BB6Q','01KZQQWTZZCCDTG8P1ZYMDJKSQ'],'limit':'此前已报告fork accepted却无新execution；重复用户消息合并一次纠正机会，不把重复fork或无法执行自动当作认知失误。'},
 {'id':'L08','type':'later-owner-vs-reviewer-correction','anchors':['01M1NJPH00SNX2PKBSVVMKA4RH','01M1NJWYCQYTYXT37FCD004M8X','01M1NK31VXQ3AJMG0CQDG1Y3F0','01M1NK72402T0YWKAP4PT9ZWZJ'],'limit':'保留原生failed及承认处置问题；后续runtime重启/无reopen限制排除，不能要求越权恢复。'},
 {'id':'L09','type':'later-responsibility-continuation-with-native-new-task','anchors':['01M1NR44ZFJ04S7HKR6HT3XYYS','01M1NR4CJ6CATJGP1T9EKMM7SK','01M1NRAFM5RP2N4TWS32W8JVJQ','01M1NRC54NBS3EBES8CTBPA7MP','01M1NRCMXMXT0RX58VWDAK8EZQ','01M1NRFGD1F1NF8XPQN4F9D9E2'],'limit':'先行动指令后新task，身份提醒再晚1.614726秒；不能归因第二条提醒。后继task错基线failed，既不算最终成功也不另算身份错误。'}]
q=[];records=[];convs=set()
for case in cases:
 for mid in case['anchors']:
  sql='SELECT * FROM messages WHERE id=?';rows=[dict(x) for x in c.execute(sql,(mid,))];assert len(rows)==1,mid
  d=rows[0];convs.add(d['conversation_id']);dump('raw/records/'+mid+'.json',d)
  records.append({'case':case['id'],'id':mid,'timestamp':d['created_at'],'sender':d['sender_identity_id'],'file':'raw/records/'+mid+'.json','json_pointer':'','sha256':hashlib.sha256((R/'raw/records'/(mid+'.json')).read_bytes()).hexdigest()})
  q.append({'sql':sql,'args':[mid],'rows':1})
for cid in sorted(convs):
 rows=[dict(x) for x in c.execute('SELECT * FROM messages WHERE conversation_id=? ORDER BY created_at,id',(cid,))];dump('raw/conversation-'+cid+'.json',rows);q.append({'sql':'SELECT * FROM messages WHERE conversation_id=? ORDER BY created_at,id','args':[cid],'rows':len(rows),'note':'context container, not all messages coded'})
taskids=['task-0e398848','task-87bd357d','task-2fbc52af'];tasks={}
for name,key in [('pm_tasks','id'),('pm_task_action_logs','task_id')]:
 sql='SELECT * FROM '+name+' WHERE '+key+' IN (?,?,?)';rows=[dict(x) for x in c.execute(sql,taskids)];dump('raw/'+name+'-september.json',rows);tasks[name]=rows;q.append({'sql':sql,'args':taskids,'rows':len(rows)})
for tid in taskids:
 sql='SELECT * FROM events WHERE refs LIKE ? ORDER BY occurred_at,seq';rows=[dict(x) for x in c.execute(sql,('%'+tid+'%',))];dump('raw/events-'+tid+'.json',rows);q.append({'sql':sql,'args':['%'+tid+'%'],'rows':len(rows)})
c.close();dump('derived/cases.json',cases);dump('derived/record-locators.json',records);dump('logs/expansion-queries.json',q)
def t(s):
 s=s.replace('Z','+00:00');s=re.sub(r'\.(\d+)(?=[+-]\d\d:\d\d$)',lambda m:'.'+m[1][:6].ljust(6,'0'),s);return dt.datetime.fromisoformat(s)
selected=[('L01','dev2: externalized recovery','2026-07-22T07:21:37.894896Z','correction'),('L02','integration: externalized delivery','2026-07-22T16:53:41.282145Z','correction'),('L03','Identity / review prompts revised','2026-07-23T01:38:22Z','source-change'),('L04','Startup actor corrected','2026-08-06T01:45:22.745154Z','correction'),('L05','Owns delivery; rejects weak evidence','2026-08-07T07:31:07.844252Z','positive'),('L08','tester3: owner vs reviewer','2026-09-04T06:46:32.704428Z','correction'),('L09','dev3: continuation requested','2026-09-04T08:24:49.541359Z','correction')]
detail=[('Failed task recorded','2026-09-04T08:21:22.031764Z','native'),('Human: create a task and continue','2026-09-04T08:24:49.541359Z','user'),('Corrective task CREATED','2026-09-04T08:25:42.726983Z','native'),('Human: executor is part of you','2026-09-04T08:25:44.341709Z','user'),('Agent: rejection is not demand termination','2026-09-04T08:26:00.500255Z','message'),('Corrective task STARTED','2026-09-04T08:26:03.681327Z','native'),('Agent: no spectator separation','2026-09-04T08:27:34.177741Z','message')]
# DB pm_tasks created_at and domain-event occurred_at differ by 0.000691s.
# Keep both native clocks; figure and prose use the domain event's occurred_at.
created=next(x['created_at'] for x in tasks['pm_tasks'] if x['id']=='task-2fbc52af')
dump('derived/timeline.json',{'selected': [{'case':a,'label':b,'time':d,'type':e} for a,b,d,e in selected], 'september': [{'label':a,'time':b,'type':d,'minutes_since_failed':(t(b)-t(detail[0][1])).total_seconds()/60} for a,b,d in detail], 'new_task_created_at':created,'seconds_action_instruction_to_task_creation':(t(created)-t(detail[1][1])).total_seconds(),'seconds_task_creation_before_identity_reminder':(t(detail[3][1])-t(created)).total_seconds(),'boundary':'Selected records, not incidence trend; source change is not activation; new task later failed at 11:05:05 for a different reported reason.'})
timeline=json.loads((R/'derived/timeline.json').read_text())
timeline.update({'seconds_fields_clock':'pm_tasks.created_at','figure_and_prose_clock':'events.occurred_at','new_task_created_event_at':detail[2][1],'seconds_action_instruction_to_created_event':(t(detail[2][1])-t(detail[1][1])).total_seconds(),'seconds_created_event_before_identity_reminder':(t(detail[3][1])-t(detail[2][1])).total_seconds()})
dump('derived/timeline.json',timeline)
print(json.dumps({'selected_message_records':len(records),'case_groups':len(cases),'expanded_conversations':len(convs),'queries':len(q)},ensure_ascii=False))
