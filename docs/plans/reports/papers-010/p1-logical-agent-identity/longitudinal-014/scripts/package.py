"""Export revision-014 derived evidence; never export private conversation text."""
from pathlib import Path
import re, json, shutil, hashlib, difflib
R=Path('/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/014-p1-longitudinal')
W=Path('/Users/oopslink/works/codes/oopslink/agent-center-research-papers')
REL=Path('docs/plans/reports/papers-010/p1-logical-agent-identity')
P=W/REL
D=P/'longitudinal-014'
def dump(p,x):
 p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n')
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
# Once-only table renumbering: preserve the order of nine main-text tables.
p=P/'paper.md';s=p.read_text()
caption='**表2。多时点责任表述与人工纠偏的精选记录**'
if caption not in s:
 s=re.sub(r'表([2-8])(?!\d)',lambda m:'表'+str(int(m[1])+1),s)
 s=s.replace('| 时间与材料 | 观察到的责任关系 | 对纵向解释的约束 |',caption+'\n\n| 时间与材料 | 观察到的责任关系 | 对纵向解释的约束 |')
 p.write_text(s)
for name in ['cases.json','record-locators.json','timeline.json','construct-audit.json','construct-audit-rows.json']:
 (D/'derived').mkdir(exist_ok=True);shutil.copy2(R/'derived'/name,D/'derived'/name)
for name in ['discovery.json','expansion-queries.json']:
 shutil.copy2(R/'logs'/name,D/name)
for name in ['protocol.md','process.md']:
 shutil.copy2(R/name,D/name)
for p in (R/'scripts').glob('*.py'):
 (D/'scripts').mkdir(exist_ok=True);shutil.copy2(p,D/'scripts'/p.name)
for p in (R/'figures').glob('p1-fig5-*'):
 shutil.copy2(p,P/'figures'/p.name)
shutil.copy2(R/'figures/environment.json',D/'figure-environment.json')
for name in ['review-final.md','review-final.json']:
 shutil.copy2(R/name,D/name)
(P/'FIGURES.md').write_text('''# P1图表与数据

当前5图、9张主文表。图1–4及原实验数值保持不变；014新增图5和表2，呈现多时点人工纠偏与9月恢复链条。原表2–8顺延为表3–9。

[014修改记录](REVISION-014.md) · [纵向分析](longitudinal-analysis.md) · [014派生证据包](longitudinal-014/README.md) · [原实验报告](controlled-experiments.md) · [原实验底表](experiment-data.json) · [原逐行评分](experiment-results.csv) · [原实验来源](experiment-provenance.json)。

图5来自[timeline.json](longitudinal-014/derived/timeline.json)，[figure.py](longitudinal-014/scripts/figure.py)生成同源PNG/SVG/PDF。左图是精选日期记录，不是发生率趋势；右图将原生状态、用户指令和Agent报告分开。新Task的创建领域事件发生在具体行动指令之后、身份提醒之前，后来仍失败，不能据此声称身份提醒单独造成恢复或最终交付。底表分别标明领域事件时间与Task.created_at，图与正文采用前者。

原图4仍可用[make_figure.py](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/make_figure.py)复算；图1–3的历史统计来源仍是figure-data.json及figure-data-provenance.json。合成情境响应不等于真实交付率，精选材料不等于全体错误或人工干预次数。全部研究尚未人工复核。
''')
# Historical counting convention: Han chars + English lexical tokens.
s=(P/'paper.md').read_text();body=s.split('## 1 引言',1)[1].split('## 参考文献',1)[0]
body='## 1 引言'+body
def clean(t):
 t=re.sub(r'<!--[\s\S]*?-->','',t)
 t=re.sub(r'!?\[([^\]]*)\]\([^)]+\)',r'\1',t)
 return re.sub(r'https?://\S+','',t)
def count(t):
 t=clean(t)
 return len(re.findall(r'[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]',t))+len(re.findall(r"[A-Za-z]+(?:['’-][A-Za-z]+)*",t))
prose='\n'.join(line for line in body.splitlines() if not re.match(r'^\s*(?:#|\||!\[|\*\*(?:图|表)\d)',line))
tables=[int(x) for x in re.findall(r'^\*\*表(\d+)。',body,re.M)]
assert tables==list(range(1,10)),tables
figures=len(re.findall(r'^!\[',body,re.M));assert figures==5
bc=json.loads((P/'body-count.json').read_text());bc.update({'revision':'014-p1-longitudinal','prose_count':count(prose),'text_including_tables_captions_headings':count(body),'figures':figures,'tables':len(tables),'longitudinal_supplement':'longitudinal-analysis.md','count_note':'Longitudinal evidence and interpretation revision; original 6000 target remains historical, not re-certified. Reproducible count in longitudinal-014/scripts/package.py.'})
dump(P/'body-count.json',bc)
(P/'REVISION-014.md').write_text('''# P1 revision-014：恢复责任中断与人工纠偏的纵向解释

2026-11-03（America/Los_Angeles）。作者 oopslink，无资助。本轮仅修订P1及论文索引，不启动新模型实验。原文和修改过程保存在本地[完整档案](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/014-p1-longitudinal/README.md)。

作者指出原结论没有准确反映使用经历：早期反复将executor视为自身之外的主体，失败后没有主动继续处理；经多次人工提醒才逐渐改善，之后仍偶发。原稿将“架构净因果未识别”及短题实验的零差异放在过强的位置，弱化了已经有记录支持的责任断裂。本轮修正这一推论，未将作者回顾改造成发生率统计。

| 修订 | 证据与边界 |
|---|---|
| 重写摘要、引言、发现及结论 | 早期多主体责任外推、提示工程干预、后期正确承责与再次纠偏并存 |
| 增补9组材料和图5、表2 | 34条原生消息定位、9个会话容器、3个九月Task及6条动作；目的性选择，非全量编码 |
| 区分两类人工提醒 | 九月新Task在具体行动指令之后、身份提醒之前创建；保留后来失败的反证 |
| 收窄原实验解释 | 512/512输入已明确修复责任，64/64修复题给出精确且获授权的动作；未测长期无提醒承责 |
| 保留bug排除和权限边界 | 技术事故后果不归因于身份；可分离的责任解释仍保留；合理升级不视为失责 |
| 保留原结果和引用 | 原547次正式调用及评分不改写；参考文献逐字不变，没有本轮新的联网核验 |

正文、机制分析及实验附录同步修订；精确[diff](revision-014.diff)、派生底表、脚本、前后哈希及[校验结果](longitudinal-014/validation.json)保存。原表2–8顺延为3–9，原图1–4未变。

分工AI审阅发现两处小问题：负结果曾写为“证明没有影响”，现改为固定题集“未检出差异”；精确间隔现明确领域事件时间与Task.created_at两种口径。审阅并非人工复核或独立重复研究。缺失的技能ARS补丁与缩写脚本没有运行，不声称通过其门禁；使用本地精确diff、哈希和范围核对替代留痕。

尚待研究的是无提醒、跨执行体和多轮失败条件下的持续承责、不同提醒方式的即时与持续效果，以及人工干预成本。抽象、角色提示、状态机、记忆和模型行为先验各自的因果贡献仍未被本轮识别。
''')
# Exact turn diff includes newly added prose but not raw private evidence.
diff=[]
modified=[p.relative_to(R/'before') for p in (R/'before').rglob('*') if p.is_file()]
new=[REL/'longitudinal-analysis.md',REL/'REVISION-014.md']
for rel in modified+new:
 before=R/'before'/rel;after=W/rel
 a=before.read_text().splitlines(True) if before.exists() else []
 b=after.read_text().splitlines(True)
 diff.extend(difflib.unified_diff(a,b,fromfile='before/'+str(rel),tofile='after/'+str(rel)))
(P/'revision-014.diff').write_text(''.join(diff));(R/'revision-014.diff').write_text(''.join(diff))
print(json.dumps({'figures':figures,'tables':len(tables),'prose_count':bc['prose_count'],'count_with_tables':bc['text_including_tables_captions_headings']},ensure_ascii=False))
