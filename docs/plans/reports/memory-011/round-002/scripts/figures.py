"""Scientific figures from derived CSVs; no private message text embedded."""
from pathlib import Path
import csv,json,collections
import numpy as np
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.colors import LogNorm
R=Path(__file__).resolve().parents[1]
plt.rcParams.update({'font.family':'DejaVu Sans','font.size':10,'axes.titlesize':11,'axes.labelsize':10,
 'axes.spines.top':False,'axes.spines.right':False,'pdf.fonttype':42,'svg.fonttype':'none','savefig.dpi':180})
def rows(p):return list(csv.DictReader((R/'derived'/p).open()))
def save(fig,name):
 for ext in ['png','svg','pdf']:fig.savefig(R/'figures'/(name+'.'+ext),bbox_inches='tight',facecolor='white')
 plt.close(fig)
p=rows('proposals.csv');rules=rows('rules.csv')
fig,axs=plt.subplots(1,2,figsize=(12.8,4.3),gridspec_kw={'width_ratios':[1,1.4]})
ax=axs[0];teams=['team-f5a633d6','team-772532f5'];labels=['Coding team (26)','Analyst team (4)']
left=np.zeros(2)
for status,color in [('promoted','#2A6F97'),('rejected','#B86145'),('pending','#B9BEC6')]:
 vals=np.array([sum(x['team_id']==t and x['status']==status for x in p) for t in teams]);ax.barh(labels,vals,left=left,color=color,label=status.capitalize(),height=.55)
 for i,n in enumerate(vals):
  if n:ax.text(left[i]+n/2,i,str(n),ha='center',va='center',color='white' if status!='pending' else '#222',weight='bold')
 left+=vals
ax.set_xlim(0,29);ax.set_xlabel('Proposals (all operations were add)');ax.invert_yaxis();ax.set_title('A   Governance outcomes',loc='left',weight='bold',pad=16)
ax.legend(ncol=3,loc='upper left',bbox_to_anchor=(-.02,-.23),frameon=False,columnspacing=1)
ax=axs[1]
groups=[('Coding: initial batch', [x for x in p if x['created_at']<'2026-08-14']),('Coding: later additions',[x for x in p if x['team_id']==teams[0] and x['created_at']>'2026-08-14']),('Analyst: reviewed',[x for x in p if x['team_id']==teams[1] and x['review_minutes']])]
for i,(label,g) in enumerate(groups):
 vals=[float(x['review_minutes']) for x in g];jit=np.linspace(-.18,.18,len(vals)) if len(vals)>1 else np.zeros(1)
 ax.scatter(vals,i+jit,s=30,color=['#2A6F97' if x['status']=='promoted' else '#B86145' for x in g],edgecolors='white',linewidths=.5,zorder=3)
 ax.plot([np.median(vals)]*2,[i-.24,i+.24],color='#161616',lw=2)
 ax.text(7000,i,f'n={len(vals)}',va='center',fontsize=9)
ax.set_xscale('log');ax.set_xlim(.08,12000);ax.set_yticks(range(3),[x[0] for x in groups]);ax.invert_yaxis();ax.set_xlabel('Proposal → recorded review (minutes, log scale)')
ax.set_title('B   Review timing varies by batch',loc='left',weight='bold',pad=16);ax.grid(axis='x',color='#e4e6e9',zorder=0)
fig.text(.02,.01,'27 reviewed proposals; black ticks = medians. Three pending proposals are right-censored at 41.87–54.01 days.\nElapsed review time is not human effort. Reviews: 26 by one agent curator, 1 by a user; 16 were author self-reviews.',fontsize=9,color='#444')
fig.subplots_adjust(bottom=.29,wspace=.6,top=.87);save(fig,'01-governance')

short={
'contract-to-production-chain-and-auditable-integration':'Contract → production',
'verify-world-state-after-actions':'Read back state',
'origin-main-required-before-completion':'Main before completion',
'intermediate-candidate-nodes-complete-before-final-main-integration':'Intermediate-node exception',
'evidence-before-verdict':'Evidence before verdict',
'plan-node-description-must-be-executable-contract':'Executable node contract',
'production-like-testing-must-use-isolated-instance':'Isolated production-like tests',
'production-evidence-report-for-runnable-but-not-started':'Report deployment evidence',
'stop-repeated-evolution-without-acceptance-progress':'Stop unproductive evolution',
'hot-topic-radar-work-rules':'Analyst work rules'}
order=sorted(rules,key=lambda x:-int(x['receipt_count']));slugs=[x['rule_slug'] for x in order]
m=rows('agent-rule-matrix.csv');agents=sorted(set(x['agent_name'] for x in m),key=lambda a:-sum(int(x['receipts']) for x in m if x['agent_name']==a))
arr=np.array([[sum(int(x['receipts']) for x in m if x['agent_name']==a and x['rule_slug']==s) for a in agents] for s in slugs])
fig,(ax,bx)=plt.subplots(1,2,figsize=(14,6.6),gridspec_kw={'width_ratios':[2.25,1]})
masked=np.ma.masked_where(arr==0,arr);cmap=plt.get_cmap('Blues').copy();cmap.set_bad('#f3f4f6')
ax.imshow(masked,cmap=cmap,norm=LogNorm(vmin=1,vmax=arr.max()),aspect='auto')
for (i,j),v in np.ndenumerate(arr):
 if v:ax.text(j,i,str(v),ha='center',va='center',fontsize=9,color='white' if v>=8 else '#222')
def name(s):return s.replace('agent-center-','').replace('integration-dev','integration').replace('hot-topic-radar-','')
ax.set_xticks(range(len(agents)),[name(a) for a in agents],rotation=50,ha='right');ax.set_yticks(range(len(slugs)),[short[s] for s in slugs]);ax.tick_params(length=0)
ax.set_title('A   Deduplicated body-read receipts by rule and agent',loc='left',weight='bold',pad=14)
for spine in ax.spines.values():spine.set_visible(False)
ys=np.arange(len(order));hours=[float(x['hours_promotion_to_first_audit']) for x in order]
bx.scatter(hours,ys,color='#2A6F97',s=40,zorder=3)
bx.set_xscale('log');bx.set_xlim(.009,500);bx.set_ylim(len(order)-.5,-.5);bx.set_yticks(ys,['']*len(ys));bx.grid(axis='x',color='#e4e6e9')
for i,h in enumerate(hours):bx.annotate(f'{h:.2f} h' if h>=1 else f'{h*60:.1f} min',(h,i),xytext=(7,0),textcoords='offset points',va='center',fontsize=9)
bx.set_xlabel('Promotion → first observed receipt\n(hours, log scale)');bx.set_title('B   First observed access',loc='left',weight='bold',pad=14)
fig.text(.015,.015,'230 receipts • 12 agents • 63 Supervisor runtime-generation contexts + 4 execution contexts • 10 rules\nReceipt = successful server-side body lookup and audit, deduplicated by context × team × commit × rule.\nZero cells mean no recorded receipt, not no reading. Timing is not first-ever use; audit coverage begins 14 Aug 2026.',fontsize=9,color='#444')
fig.subplots_adjust(left=.24,right=.97,top=.89,bottom=.25,wspace=.2);save(fig,'02-access')
(R/'figures/environment.json').write_text(json.dumps({'matplotlib':matplotlib.__version__,'numpy':np.__version__},indent=2)+'\n')
print('Rendered 2 figures in PNG, SVG, PDF.')
