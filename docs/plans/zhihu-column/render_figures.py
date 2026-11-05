"""Editorial figures: editable SVG + 2x PNG. Sources and limits: evidence.md.
Run: python docs/plans/zhihu-column/render_figures.py
"""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont
from html import escape
import math

ROOT=Path(__file__).resolve().parent
OUT=ROOT/'figures'; OUT.mkdir(exist_ok=True)
W,H,S=1600,1040,2
INK='#152C40'; MUTED='#536779'; BLUE='#245DD8'; TEAL='#087F79'; RED='#B84B36'; GOLD='#9B681D'
LINE='#D8E1E8'; BG='#FAFBFC'; LIGHT='#EDF3FA'; GREEN='#EAF5F2'; WARM='#FAEEE9'
FONT='/System/Library/Fonts/STHeiti Medium.ttc'
ALL=[]
class Figure:
 def __init__(self,name,title,sub,kind,source):
  self.name=name; self.title=title; self.source=source; self.kind=kind
  self.im=Image.new('RGB',(W*S,H*S),BG);self.d=ImageDraw.Draw(self.im);self.svg=[]
  self.rect(0,0,12,H,BLUE)
  self.text(64,32,'AGENT 协作手记  /  '+name[:2],18,BLUE)
  self.text(1536,32,kind,18,MUTED,anchor='end')
  self.text(64,83,title,44,INK)
  self.text(64,151,sub,23,MUTED)
  self.line(64,207,1536,207,LINE,1)
  self.line(64,968,1536,968,LINE,1)
  self.text(64,985,source,18,MUTED)
  self.text(1536,985,'agent-center  /  '+name[:2],18,MUTED,anchor='end')
 def rect(self,x,y,w,h,fill='white',stroke=None,r=0):
  coords=tuple(round(z*S) for z in (x,y,x+w,y+h))
  if r:self.d.rounded_rectangle(coords,r*S,fill=fill,outline=stroke,width=2*S)
  else:self.d.rectangle(coords,fill=fill,outline=stroke,width=2*S)
  self.svg.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{r}" fill="{fill}" stroke="{stroke or "none"}"/>')
 def line(self,x,y,x2,y2,c=LINE,width=2,dash=False):
  if dash:
   dist=math.hypot(x2-x,y2-y)
   for i in range(0,int(dist),15):
    a=i/dist;b=min(i+8,dist)/dist
    self.d.line([(round((x+(x2-x)*a)*S),round((y+(y2-y)*a)*S)),(round((x+(x2-x)*b)*S),round((y+(y2-y)*b)*S))],fill=c,width=width*S)
  else:self.d.line((x*S,y*S,x2*S,y2*S),fill=c,width=width*S)
  self.svg.append(f'<line x1="{x}" y1="{y}" x2="{x2}" y2="{y2}" stroke="{c}" stroke-width="{width}"'+(' stroke-dasharray="8 7"' if dash else '')+'/>')
 def arrow(self,x,y,x2,y2,c=BLUE,dash=False):
  self.line(x,y,x2,y2,c,3,dash)
  a=math.atan2(y2-y,x2-x);pts=[(x2,y2),(x2-12*math.cos(a-.5),y2-12*math.sin(a-.5)),(x2-12*math.cos(a+.5),y2-12*math.sin(a+.5))]
  self.d.polygon([(int(u*S),int(v*S)) for u,v in pts],fill=c)
  self.svg.append('<polygon points="'+' '.join(f'{u},{v}' for u,v in pts)+f'" fill="{c}"/>')
 def circle(self,x,y,r,c):
  self.d.ellipse(((x-r)*S,(y-r)*S,(x+r)*S,(y+r)*S),fill=c)
  self.svg.append(f'<circle cx="{x}" cy="{y}" r="{r}" fill="{c}"/>')
 def text(self,x,y,t,size=26,c=INK,anchor='start'):
  font=ImageFont.truetype(FONT,size*S)
  for n,row in enumerate(t.split('\n')):
   yy=y+n*size*1.5
   width=self.d.textlength(row,font=font)/S
   xx=x-width if anchor=='end' else x-width/2 if anchor=='middle' else x
   if xx<0 or xx+width>W:raise ValueError((self.name,row,xx,width))
   self.d.text((xx*S,yy*S),row,font=font,fill=c)
   self.svg.append(f'<text x="{x}" y="{yy+size*.85}" font-size="{size}" fill="{c}" text-anchor="{anchor}">{escape(row)}</text>')
 def card(self,x,y,w,h,kicker,title,body,c=BLUE):
  self.rect(x,y,w,h,'white',LINE,12);self.rect(x,y,5,h,c)
  self.text(x+25,y+20,kicker,18,c);self.text(x+25,y+58,title,30)
  self.text(x+25,y+111,body,24,MUTED)
 def note(self,title,body,c=TEAL):
  self.rect(64,830,1472,106,GREEN if c==TEAL else WARM,None,8)
  self.text(88,848,title,25,c);self.text(88,889,body,23,INK)
 def save(self):
  self.im.save(OUT/(self.name+'.png'))
  doc=f'<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="1040" viewBox="0 0 1600 1040"><title>{escape(self.title)}</title><desc>{escape(self.source)}</desc><style>text{{font-family:"Heiti SC","PingFang SC","Microsoft YaHei",sans-serif}}</style><rect width="1600" height="1040" fill="{BG}"/>'+''.join(self.svg)+'</svg>'
  (OUT/(self.name+'.svg')).write_text(doc)
  ALL.append((self.name,self.title,self.kind))

def fig(name,title,sub,kind='机制示意',source='来源：正文所列代码与设计记录；示意图不代表运行测量。'):
 return Figure(name,title,sub,kind,source)
def lanes(f,labels,xs):
 for label,x in zip(labels,xs):
  f.rect(x-130,245,260,64,LIGHT,None,8);f.text(x,263,label,27,BLUE,'middle');f.line(x,325,x,780,LINE,2,True)
def event(f,a,b,y,label,c=BLUE):
 f.arrow(a,y,b,y,c);f.text((a+b)/2,y-40,label,22,c,'middle')

# 02: historical relationship; responsibility chain; synchronization; recovery trace.
f=fig('02-workitem-origin','一项任务，先后两段承接关系','Task 保持业务连续性；WorkItem 记录某个 Agent 的处理过程。','历史模型','来源：删除前 work_item.go（2b6af1de）；T、W-A、W-B 为说明用编号。')
f.rect(64,250,1472,134,LIGHT,None,12);f.text(96,275,'TASK T  ·  修复登录超时',32,BLUE);f.text(96,329,'目标、任务编号与对话保留；负责人由 A 转为 B',25)
f.line(410,384,410,455,BLUE);f.line(1190,384,1190,455,BLUE)
f.card(64,455,650,296,'分配给 A','WorkItem W-A','排队 → 处理 → 等待回复 → 继续\n同一工作项可以跨多轮交互\n转派后：superseded（被取代）',TEAL)
f.card(886,455,650,296,'转派给 B','WorkItem W-B','新建工作项，从排队开始\nagentID 改为 B\ntaskRef 仍然指向任务 T',BLUE)
event(f,730,870,600,'转派')
f.note('需要区分的三件事','业务任务的连续性、Agent 承接关系的连续性、执行进程的连续性并不相同。');f.save()

f=fig('02-task-boundaries','任务状态与执行事实，需要分段核对','从业务记录到实际执行，中间每一步都有独立证据。')
for x,k,t,b,c in [(64,'01 / 业务','Task','当前负责人 / 任务状态\n阻塞原因 / 执行租约\n动作历史 / 计划关联',BLUE),(574,'02 / 调度','可执行性与唤醒','list_my_tasks：发现工作\nstart_task：检查门禁\nwork_available：通知',TEAL),(1084,'03 / 运行','Runtime','Supervisor 判断与分派\nExecutor 创建与运行\n结果返回 / 后续交付',BLUE)]:f.card(x,292,452,350,k,t,b,c)
f.arrow(525,465,564,465);f.arrow(1035,465,1074,465)
f.text(64,704,'已分配  ≠  可以启动  ≠  收到通知  ≠  执行已创建  ≠  交付完成',29,RED)
f.note('模型收敛的边界','Task 统一工作管理；运行层继续保存进程、执行与结果的事实。');f.save()

f=fig('02-state-sync','旧模型的维护成本，出现在双向联动中','不同状态有其语义；复杂度来自跨模型转换需要同步裁决。','历史机制','来源：2b6af1de 的 work_item_projector.go 与 task_status_sync_projector.go。')
f.card(64,260,420,380,'PROJECT MANAGEMENT','Task','分配 / 转派\n开始 / 完成\n失败后的业务阻塞',BLUE)
f.card(1116,260,420,380,'AGENT','WorkItem','创建 / 取代旧项\n激活 / 结束\n等待 / 失败',TEAL)
event(f,500,1100,350,'分配 / 转派 → 更新工作项')
event(f,1100,500,535,'激活 / 失败 → 推进任务',TEAL)
f.rect(548,616,504,145,WARM,None,10);f.text(575,636,'同一个“Task 完成”事件',25,RED);f.text(575,681,'active → done\nqueued / waiting_input → canceled',23)
f.note('幂等并不替代业务规则','事件重复消费可以被防护；两套状态机的合法映射仍需要单独定义和维护。',RED);f.save()

f=fig('02-recovery-trace','“进行中”卡住时，沿责任链寻找断点','排障示意：先确认事实，再判断下一步由谁驱动。','诊断路径')
for i,(t,b) in enumerate([('任务可运行？','状态 / 依赖'),('通知已送达？','唤醒 / 注入'),('已作出分派？','Supervisor 决定'),('执行已创建？','fork / 运行记录'),('结果已交回？','候选 / 验收')]):
 x=64+i*300;f.circle(x+26,318,24,BLUE);f.text(x+26,303,str(i+1),23,'white','middle');f.text(x,370,t,27);f.text(x,423,b,22,MUTED)
 if i<4:f.arrow(x+70,318,x+278,318)
f.line(815,482,815,550,RED);f.rect(534,550,650,194,WARM,None,12);f.text(565,578,'例：有 running，没有新的 execution',28,RED);f.text(565,633,'先核对显式分派是否发生\n重复写入任务状态，不能代替创建执行',25)
f.note('恢复需要动作主体','找到停在哪一步，也要找到谁有权、谁有足够信息执行下一步。');f.save()

# 03: architecture, routing, analytic cost curve.
f=fig('03-execution-lanes','一次委派如何交回同一个负责人','临时执行可以结束；任务的剩余交付义务仍需要有人接住。')
xs=[240,800,1360];lanes(f,['任务系统','Supervisor','Executor'],xs)
event(f,240,800,380,'通知有工作');event(f,800,1360,485,'选择模型，创建执行')
event(f,1360,800,610,'返回候选 / 失败 / 验收缺口',TEAL);event(f,800,240,735,'判断结果，继续推进任务',TEAL)
f.note('模型选择与责任交接是两项决定','选谁执行，决定执行成本；怎样接回结果，决定目标能否真正交付。');f.save()

f=fig('03-model-routing','模型选择：按代码中的回退顺序读取','智能判断是可选分支；配置与确定性回退同样决定最终结果。','代码分支','来源：Router.ResolveExecutor / Judge / executor_runtime；细节见正文源码链接。')
items=[('任务指定','Task 明确指定模型；存在候选池时需匹配'),('可选判断','Judge 启用且候选池非空：结合能力与价格选择'),('默认配置','default_executor_model；未配置时参考 orchestrator_model'),('候选回退','候选池首项提供确定性的可用选择'),('最终回退','Supervisor 自身模型；仍无法解析则报告错误')]
for i,(a,b) in enumerate(items):
 y=250+i*108;f.circle(95,y+28,25,TEAL if i==1 else BLUE);f.text(95,y+12,str(i+1),25,'white','middle');f.text(150,y+7,a,28);f.text(405,y+10,b,24,MUTED)
 if i<4:f.line(95,y+58,95,y+81,LINE)
f.note('判断开关默认关闭','实现了模型选择机制，不等于已经证明它达到了最低成本或最佳效果。',RED);f.save()

f=fig('03-cost-threshold','便宜模型的优势，会被返工逐步吃掉','假设：直接强模型成本 10；路由判断 1，首次执行 4，失败后重做 10。','说明性算例','来源：正文假设；C直接 = 10，C路由 = 5 + 10p。不是项目实测价格或失败率。')
x0,y0,x1,y1=190,735,1230,280
for v in [5,10,15]:
 y=y0-(v-5)/10*(y0-y1);f.line(x0,y,x1,y,LINE);f.text(160,y-15,str(v),24,MUTED,'end')
for p in [0,.25,.5,.75,1]:
 x=x0+p*(x1-x0);f.text(x,758,f'{p:.0%}',23,MUTED,'middle')
f.line(x0,y0,x0,y1,INK);f.line(x0,y0,x1,y0,INK)
f.line(x0,(y0+y1)/2,x1,(y0+y1)/2,BLUE,4)
f.line(x0,y0,x1,y1,TEAL,4);f.circle((x0+x1)/2,(y0+y1)/2,8,RED)
f.text(1260,285,'路由后执行',23,TEAL);f.text(1260,500,'直接强模型',23,BLUE)
f.text(730,545,'p = 50%\n两者成本持平',25,RED);f.text(190,239,'期望成本 / 单位',23,MUTED);f.text(1230,797,'重做概率 p →',22,MUTED,'end')
f.note('比较最终交付成本','还需纳入人工介入、恢复上下文与等待时间；单次调用便宜并不等于整体节省。');f.save()

# 04: data, cost data, handoff contract, failure sequence.
f=fig('04-workflow-results','增加角色，没有增加本组实现通过数','两角色与四角色：各 12 次执行；共同控制者不计入角色数。','实验数据','来源：P2 §4.8–4.10；12 对探索性执行，不能外推为所有协作任务。')
for col,(title,vals) in enumerate([('实现检查通过',[12,12]),('完整交付',[12,11])]):
 x=64+col*760;f.text(x,260,title,31)
 for row,v in enumerate(vals):
  y=355+row*150;f.text(x,y,'两角色' if row==0 else '四角色',26)
  for k in range(12):f.rect(x+k*51,y+52,38,45,TEAL if k<v else '#DDE4EB',None,5)
  f.text(x+650,y+55,f'{v}/12',27,TEAL)
f.rect(64,704,1472,83,LIGHT,None,8);f.text(92,726,'额外决策 91 次   =   控制端 +52   +   工作端 +39',31,BLUE)
f.note('结论限于本组任务','新增角色带来更多交接；独立检查是否值得保留，还要看它发现了什么缺陷。');f.save()

def bars(name,title,sub,rows,maxv,ticks,unit,source,note):
 f=fig(name,title,sub,'数据对照',source);x0=510;x1=1380
 for v in ticks:
  x=x0+(x1-x0)*v/maxv;f.line(x,290,x,760,LINE,1);f.text(x,775,f'{v:g}{unit}',21,MUTED,'middle')
 for i,(label,value,display) in enumerate(rows):
  y=322+i*108;f.text(64,y,label,27);w=(x1-x0)*value/maxv;f.rect(x0,y,w,42,TEAL,None,3);f.text(x0+w+16,y+5,display,24,TEAL)
 f.note('读图边界',note);f.save()
prices=[('Sonnet / 分表',.29474,.49811),('Sonnet / 联表',.31704,.45627),('Haiku / 分表',.01810,.03687),('Haiku / 联表',.02258,.02784)]
bars('04-price-change','增加角色后，同组平均费用变化','同一模型、同一视图下：四角色均价 ÷ 两角色均价 − 1。',[(n,(b/a-1)*100,f'+{(b/a-1)*100:.1f}%') for n,a,b in prices],120,[0,30,60,90,120],'%', '来源：P2 表 9；每格均价来自三项任务，按实验报告价计算。','各组基准价格不同。相对涨幅大，不等于绝对多花的钱最多。')

f=fig('04-handoff-contract','一次可核验的交接，需要交出什么','建议的交接结构：让接收者能判断是否沿用证据、是否需要重跑。','协议建议','来源：P2 回执认可案例与正文建议；字段示意，不是现有页面截图。')
f.card(64,265,380,470,'发送方','完成本环节','固定候选版本\n整理检查产物\n指出未完成要求',BLUE)
f.rect(510,265,580,470,'white',LINE,12);f.text(542,293,'交接包 / HANDOFF',28,TEAL)
for i,(a,b) in enumerate([('候选','哪一版代码 / commit'),('依据','哪份需求 / 验收要求'),('证据','命令、结果、产物位置'),('剩余义务','还差什么 / 谁继续')]):
 y=360+i*82;f.text(542,y,a,24,TEAL);f.text(710,y,b,24);f.line(542,y+50,1058,y+50,LINE,1)
f.card(1156,265,380,470,'接收方','作出下一步判断','确认候选一致\n确认检查适用\n沿用证据或补查',BLUE)
f.arrow(452,490,500,490);f.arrow(1100,490,1146,490)
f.note('需要共享的是可核验的事实','仅传递“我做完了”或“测试通过”，不足以支持下一环节的判断。');f.save()

f=fig('04-review-loop','七张成功回执，为什么仍未交付','同一版本的真实检查结果，被“必须来自我的会话”反复否定。','案例复盘','来源：P2 表 10；唯一未闭合的时效过滤执行。计数来自该案例。')
f.card(64,275,450,335,'01 / 已有成果','实现与测试通过','实现检查 8/8\n自编测试 6/6\n候选已具备检查结果',TEAL)
f.card(574,275,450,335,'02 / 重复审查','回执认可反复受阻','审查者共启动 4 次\n七张真实成功回执\n同一候选未改变',RED)
f.card(1084,275,452,335,'03 / 预算耗尽','集成没有发生','6 次启动预算用完\n集成者启动 0 次\n缺少最终交付清单',RED)
f.arrow(525,430,562,430,RED);f.arrow(1035,430,1073,430,RED)
f.line(800,610,800,700,RED);f.arrow(800,700,600,700,RED);f.text(820,679,'反复换回执，没有新增缺陷发现',25,RED)
f.note('失败发生在证据认可协议','检查真实与否，和检查是否由当前会话亲自产生，是两个不同问题。',RED);f.save()

# 05: identity boundaries, material selector, evidence counts.
f=fig('05-identity-map','名字、身份、权限与材料，各自回答一个问题','同一个 Agent 改名、重启或换任务时，这些关系仍需保持清晰。')
for x,y,k,t,b,c in [(64,270,'识别','Identity / 稳定主体','保存稳定 ID\n显示名可变，历史关联保留',BLUE),(824,270,'授权','Member / 组织关系','记录成员角色与状态\n允许的动作由授权规则决定',TEAL),(64,540,'执行','Agent / 运行配置','模型与工作节点等配置\n进程变化不应抹掉业务身份',BLUE),(824,540,'判断','上下文 / 本次可见材料','当前任务、候选与检查记录\n有权限，不等于材料已充分',TEAL)]:f.card(x,y,712,235,k,t,b,c)
f.note('设计判断','把识别、授权与信息供给分开核对，才能解释“同一个成员为什么做不了下一步”。');f.save()

f=fig('05-context-selection','先筛当前问题，再决定读哪些历史','说明例：当前要验收候选 B，手里却有候选 A 的绿色报告。','说明性示例','来源：正文情境；A/B 为假设版本，图中筛选步骤是设计建议。')
f.card(64,290,455,440,'材料来源','两份记录','同角色 Tester：\n候选 A，检查通过\n\n跨角色开发者：\n候选 B，检查失败',BLUE)
f.card(584,290,432,440,'选择条件','对当前判断是否适用','① 是否有权读取\n② 是否对应候选 B\n③ 是否覆盖本轮要求\n④ 是否仍然有效',TEAL)
f.card(1081,290,455,440,'判断依据','当前失败必须可见','只读同角色历史\n可能留下旧的绿色报告\n\n按任务与版本选材料\n才能解释 B 的具体缺口',RED)
f.arrow(530,490,572,490);f.arrow(1027,490,1069,490)
f.note('角色相同，不保证信息充分','选择单位应是当前判断所需的证据，并始终受权限约束。');f.save()

f=fig('05-context-coverage','不同材料范围，能支持多少类情境？','每格计 1 类情境，仅表示数量；不同列不代表同一情境的配对。','实验数据','来源：P3 表 7；8 类构造情境，衡量材料充分性，不是端到端交付率。')
for i,(label,n) in enumerate([('个人持有历史',3),('同材料生产者角色',2),('全部获准记录',8),('当前任务范围',8)]):
 y=290+i*122;f.text(64,y+15,label,29)
 for k in range(8):
  x=570+k*100;f.rect(x,y,74,64,TEAL if k<n else '#E2E8ED',None,7)
  if k<n:
   f.line(x+23,y+32,x+33,y+42,'white',3);f.line(x+33,y+42,x+53,y+21,'white',3)
 f.text(1450,y+15,f'{n}/8',30,TEAL,'middle')
f.note('同角色材料最少，仅覆盖 2/8 类','按当前任务范围筛选，在本组情境中保持 8/8，同时减少无关材料。');f.save()

# 06: two lineages, edit transaction, repair selection, observational chart.
f=fig('06-two-lineages','计划依赖与代码继承，是两条不同的链','任务图向前推进，还需要核对后续候选是否包含已验证的修改。','概念示意','来源：正文与 P4 候选谱系分析；A/B/C 为说明符号。')
for y,title,c,labels in [(270,'工作安排 / PLAN',BLUE,['实现 A','修复 B','复验 C']),(545,'成果继承 / ARTIFACT',TEAL,['候选 A','候选 B','候选 C'])]:
 f.text(64,y,title,23,c)
 for i,label in enumerate(labels):
  x=64+i*530;f.rect(x,y+55,410,108,'white',LINE,10);f.text(x+205,y+90,label,30,c,'middle')
  if i<2:f.arrow(x+425,y+109,x+512,y+109,c,y>500)
f.text(590,470,'依赖边不能证明代码已继承',26,RED)
f.note('需要两类证据','计划检查节点与依赖；成果检查版本、文件内容及验收要求。');f.save()

f=fig('06-edit-transaction','修改计划，提交的是一个完整的新状态','批量编辑后检查最终图，同时防止旧版本覆盖与在途任务被撤回。','代码机制','来源：projectmanager/service/plan_topology_edit.go；省略存储适配细节。')
for i,(title,body) in enumerate([('读取基线','节点 / 边 / 分派\n核对 base_version'),('应用操作','在内存副本修改\n不逐步提交半张图'),('验证最终状态','同项目 / 无环\n保护在途节点'),('提交与推进','一次事务写入\n继续计划执行')]):
 x=64+i*380;f.card(x,300,332,305,f'0{i+1}',title,body,BLUE if i%2==0 else TEAL)
 if i<3:f.arrow(x+340,460,x+368,460)
f.text(64,690,'版本冲突：有人已经改过图',28,RED);f.text(824,690,'分派冲突：节点已经开始工作',28,RED)
f.note('结构合法，只是第一层保证','事务与图校验不能判断新计划是否更合理，也不能证明后续候选继承了旧成果。');f.save()

f=fig('06-repair-triage','任务未完成，先分清缺的是哪一种东西','修复前的建议检查：避免把材料交接问题再次变成代码开发。','方法建议','来源：正文反思与 P4 观察；尚未用实验验证此检查流程的收益。')
for i,(a,b,c) in enumerate([('实现行为错误','安排代码修复','修复后重验相关行为'),('实现正确，缺少复验','复验原候选','固定版本，补齐所缺检查'),('检查通过，接收者读不到','修复证据交接','补位置、权限与适用说明')]):
 y=275+i*170;f.rect(64,y,1472,132,'white',LINE,10);f.text(92,y+23,a,29,BLUE);f.arrow(530,y+66,620,y+66);f.text(660,y+23,b,29,TEAL);f.text(1040,y+28,c,25,MUTED)
f.note('先解释新增工作的必要性','每次改图应对应一个明确的交付缺口，并说明为什么已有成果不足以填补它。');f.save()

f=fig('06-topology-outcomes','改图与任务完成：观察到的是共现','每行按自身样本量归一化；两组数量相差很大，不能直接比较改图收益。','观察数据','来源：P4 表 5a；169 次父子快照转移，不是随机分组实验。')
for y,label,total,done in [(315,'节点或边改变',166,101),(555,'拓扑保持原样',3,1)]:
 f.text(64,y,label,30);f.text(64,y+51,f'n = {total} 次转移',24,MUTED)
 x=540;w=970;split=w*done/total;f.rect(x,y,split,65,TEAL);f.rect(x+split,y,w-split,65,'#DCE4EB')
 f.text(x,y+90,f'共有任务转完成：{done} 次',25,TEAL);f.text(1510,y+90,f'其余：{total-done} 次',25,MUTED,'end')
f.note('不把相关变化写成因果收益','同一区间可能发生复验、转派等操作；完成字段也不等于重新验收后的交付。',RED);f.save()

# 07 memory distribution, applicability matrix, publishing paths, conflict.
bars('07-memory-reads','230 条读取回执，分布在哪些阶段？','12 名 Agent；按首次留存回执的阶段归类。',[(a,n,f'{n}') for a,n in [('执行 execute',125),('计划 plan',38),('审查 review',40),('恢复 recovery',27)]],140,[0,35,70,105,140],'','来源：P5 §4.2；按上下文、团队、commit 与规则去重的正文回执。','这不是全部调用次数，也不是采用率；各阶段缺少应用机会的共同分母。')

f=fig('07-memory-phases','同一条规则，在哪个阶段生效？','各条件 × 各阶段 4 次；两列表示各自阶段的目标动作。','实验数据','来源：P5 表 4；共 32 次首步处置，不是端到端交付实验。')
f.text(64,260,'规则条件',25,MUTED);f.text(795,252,'中间交接\n完成交接',25,TEAL,'middle');f.text(1315,252,'最终集成\n正确暂缓',25,TEAL,'middle')
for i,(name,n) in enumerate([('无规则',4),('无条件合入 main',3),('按阶段限定',4),('新旧并列，明确替代',4)]):
 y=365+i*104;f.text(64,y+15,name,28)
 for x,val in [(630,n),(1150,4)]:
  for k in range(4):f.rect(x+k*61,y,46,56,TEAL if k<val else WARM,None,5)
  f.text(x+275,y+13,f'{val}/4',28,TEAL if val==4 else RED)
f.note('问题来自适用条件','无条件规则在本组中阻断一次中间交接；最终集成阶段各条件均正确暂缓。');f.save()

f=fig('07-memory-pipeline','写入文件之后，记忆还要经过三道边界','保存、共享、读取与采用需要分别验证。','机制与检查','来源：memory/path.go、centergit producer/consumer 与 P5；反馈为评估要求。')
for i,(t,b) in enumerate([('局部保存','任务 / 项目作用域\n留下经验与来源'),('团队发布','仓库 / 授权\n记录版本与变更'),('读取快照','阶段 / commit\n知道本次读了什么'),('行为核对','适用当前任务？\n实际采取什么动作？')]):
 x=64+i*380;f.card(x,295,332,330,f'0{i+1}',t,b,TEAL if i==3 else BLUE)
 if i<3:f.arrow(x+340,460,x+368,460)
f.text(64,700,'文件存在 ≠ 已经共享 ≠ 本次读到 ≠ 正确采用',32,RED)
f.note('稳定快照与及时更新需要取舍','运行中的执行者可能保留旧快照；应按加载、恢复与新建执行的约定核对。');f.save()

f=fig('07-rule-deadlock','缺少阶段限定，正确规则也会阻断交接','示例规则：“完成前必须合入 main”；候选交接却是独立验收的前提。','案例机制','来源：P5 宽泛 main 规则案例；图为因果关系概括，不是逐条原始事件。')
f.card(64,280,640,240,'开发环节','先交出可验收的候选','交接完成后，测试者才能开始',BLUE)
f.card(896,280,640,240,'验收与集成','先独立验收，再合入 main','候选尚未交接，后续无法开始',TEAL)
event(f,720,880,380,'依赖')
f.line(1216,520,1216,670,RED);f.arrow(1216,670,384,670,RED);f.line(384,670,384,520,RED)
f.text(800,710,'无条件规则要求先合入，反过来拦住候选交接',27,RED,'middle')
f.note('限定到最终集成阶段','中间环节交出固定、可访问的候选与必要证据；最终交付再执行合入门禁。');f.save()

# 08 evidence matrix, development cycle and review packet.
f=fig('08-evidence-levels','不同层面的主张，需要不同层面的证据','先说清楚准备证明什么，再选择验证方式。','方法建议','来源：项目规约、测试规约与正文反思；层级不是质量评分。')
f.text(64,252,'主张层面',24,MUTED);f.text(470,252,'需要核对的事实',24,MUTED);f.text(1080,252,'相应证据',24,MUTED)
for i,(a,b,c) in enumerate([('领域规则','权限 / 状态转换 / 唯一写入者','单元与契约检查'),('进程与并发','重复启动 / 重连 / 资源释放','集成、race、故障注入'),('用户路径','任务到达 / 执行 / 结果可见','真实页面与运行轨迹'),('最终交付','质量 / 成本 / 返工 / 人工介入','终态验收与同任务对照')]):
 y=325+i*110;f.rect(64,y,1472,90,'white',LINE,8);f.text(88,y+28,a,28,BLUE);f.text(470,y+30,b,25);f.text(1080,y+30,c,25,TEAL)
f.note('测试全绿，应说明绿在哪一层','覆盖率不能替代用户路径证据；用户路径成功也不能单独证明成本收益。');f.save()

f=fig('08-development-loop','一次可复核的修改，怎样形成闭环','从具体症状开始，经实现与证据返回原问题。','开发实践','来源：项目 doc-first / 领域边界 / 测试规约；流程为正文归纳。')
positions=[(64,270),(574,270),(1084,270),(1084,565),(574,565),(64,565)]
items=[('现场问题','保存症状与失败轨迹'),('设计边界','明确谁负责、什么不能改'),('小步实现','把改动限制为可验收单元'),('运行验证','核对原路径与关键风险'),('证据复核','代码、日志、页面相互对应'),('人作裁决','完成 / 修正 / 否决方案')]
for i,((x,y),(t,b)) in enumerate(zip(positions,items)):
 f.card(x,y,452,210,f'0{i+1}',t,b,BLUE if i<3 else TEAL)
f.arrow(526,375,564,375);f.arrow(1036,375,1074,375);f.arrow(1310,490,1310,555)
f.arrow(1074,670,1036,670,TEAL);f.arrow(564,670,526,670,TEAL)
f.line(64,670,36,670,TEAL,2,True);f.line(36,670,36,375,TEAL,2,True);f.arrow(36,375,64,375,TEAL,True)
f.note('Agent 可以推进实现，人保留关键裁决','对领域边界、证据标准和最终完成条件的判断，必须在开发过程中持续可见。');f.save()

f=fig('08-review-packet','把“帮我修好”变成可审查的任务','任务输入既给 Agent 行动空间，也给复核者明确的检查依据。','建议模板','来源：正文任务模板；非原始聊天记录，非真实产品截图。')
f.rect(64,260,1472,505,'white',LINE,12)
rows=[('01 症状','任务可运行，但没有活跃执行；附原始轨迹。'),('02 定位','按门禁、通知、分派、启动逐段核对。'),('03 约束','保持领域边界，防止重复创建执行。'),('04 输出','解释根因、最小修改与仍未覆盖的路径。'),('05 验收','复现原失败；提供修复后的对应证据。')]
for i,(a,b) in enumerate(rows):
 y=287+i*94;f.text(94,y,a,27,BLUE);f.text(390,y,b,26)
 if i<4:f.line(94,y+64,1500,y+64,LINE,1)
f.note('审查的是主张与证据之间的对应关系','新增了多少代码、跑了多少条命令，都不能单独回答原问题是否得到解决。');f.save()

# Contact sheet and a self-contained navigable local gallery using relative SVG assets.
thumbs=Image.new('RGB',(1600,math.ceil(len(ALL)/4)*300),'#E8EDF2');d=ImageDraw.Draw(thumbs)
for i,(name,title,kind) in enumerate(ALL):
 im=Image.open(OUT/(name+'.png'));im.thumbnail((380,247));x=10+(i%4)*400;y=10+(i//4)*300;thumbs.paste(im,(x,y));d.text((x,y+253),name,font=ImageFont.truetype(FONT,17),fill=INK)
thumbs.save(OUT/'contact-sheet.jpg',quality=94)
html='''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Agent 协作手记 · 配图审阅</title><style>body{margin:0;background:#eef2f5;color:#152c40;font-family:system-ui,sans-serif}header,main{max-width:1200px;margin:auto;padding:32px}h1{font-size:32px}nav{display:flex;gap:24px;flex-wrap:wrap}a{color:#245dd8}article{margin:36px 0 64px}img{width:100%;background:white;box-shadow:0 8px 32px #152c4010}p{line-height:1.8;color:#536779}.links{display:flex;gap:24px}h2{margin-top:64px;border-top:1px solid #cbd6df;padding-top:24px}</style><header><h1>Agent 协作手记 / 配图审阅</h1><p>25 张重绘与新增插图 · PNG 3200 × 2080 · SVG 可编辑矢量源图<br>截图与原论文图仍保留在文章中。本页按正文主题展示新增视觉体系。</p><nav>'''
for n in range(2,9):html+=f'<a href="#article-{n}">第 {n} 篇</a>'
html+='</nav></header><main>';last=None
for name,title,kind in ALL:
 n=int(name[:2])
 if n!=last:html+=f'<h2 id="article-{n}">第 {n} 篇</h2>';last=n
 html+=f'<article><h3>{escape(title)}</h3><p>{kind}</p><a href="figures/{name}.png"><img loading="lazy" src="figures/{name}.svg" alt="{escape(title)}"></a><p class="links"><a href="figures/{name}.png">高清 PNG</a><a href="figures/{name}.svg">矢量 SVG</a></p></article>'
(ROOT/'figure-gallery-vector.html').write_text(html+'</main></html>')
print(f'Rendered {len(ALL)} SVG + PNG pairs; gallery and contact sheet generated.')
