"""Readonly discovery for historical ownership corrections; not incidence coding."""
from pathlib import Path
import sqlite3,json,re,collections
R=Path(__file__).resolve().parents[1]
c=sqlite3.connect('file:/Users/oopslink/works/research/agent-harness-study/run-001/raw/snapshot.sqlite?mode=ro',uri=True);c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON')
terms=r'executor|supervisor|执行器|执行体|监督者|监工|身份认知|身份混淆'
ownership=r'自己|自身|你的|你是|你来|你负责|你的一部分|你调度|不是你|同一个|外部|身份|负责|修复|处理|confus'
rows=[];user=[];alln=0
sql='SELECT id,conversation_id,sender_identity_id,content,created_at,context_refs FROM messages ORDER BY created_at,id'
for x in c.execute(sql):
 alln+=1;d=dict(x)
 if not re.search(terms,d['content'],re.I):continue
 d['ownership_terms']=bool(re.search(ownership,d['content'],re.I));rows.append(d)
 if d['sender_identity_id'].startswith('user:'):user.append(d)
def dump(p,x):(R/p).write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
dump('raw/role-term-candidates.json',rows);dump('raw/user-role-messages.json',user)
dump('logs/discovery.json',{'sql':sql,'terms':terms,'ownership_terms':ownership,'all_messages':alln,'role_term_candidates':len(rows),'user_role_messages':len(user),'user_ownership_candidates':sum(x['ownership_terms'] for x in user),'selection':'Lexical discovery only; missing paraphrases possible; not independent errors or all human interventions.'})
for d in user:
 if d['ownership_terms']:print(d['id'],d['created_at'],d['conversation_id'],d['content'][:750].replace('\n',' / '))
c.close()
