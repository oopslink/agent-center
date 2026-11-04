# -*- coding: utf-8 -*-
import csv
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.patches import Rectangle,Patch
from matplotlib.lines import Line2D
from collections import Counter
p=Path(__file__).resolve().parent
a=list(csv.DictReader((p/'outcomes.csv').open()));pref=list(csv.DictReader((p/'prefix.csv').open()))
colors={'accepted':'#267568','escalate':'#bd632c','defer':'#54748d','horizon':'#9b8296','transport_failure':'#393f50'}
sym={'dispatch':'D','inspect':'Q','invalid':'!','defer':'W','escalate':'E','accept':'A'}
plt.rcParams.update({'font.family':'DejaVu Sans','font.size':8,'pdf.fonttype':42,'svg.fonttype':'none'})
fig,axs=plt.subplots(1,2,figsize=(7.18,4.8),gridspec_kw={'width_ratios':[1.6,1]},layout='constrained')
ax=axs[0];ax.set_title('a   Trajectories after a shared first decision',loc='left',fontweight='bold',fontsize=9)
ordered=[x for tier in ['True','False'] for x in pref if x['access']==tier]
for r,x in enumerate(ordered):
 for j,arm in enumerate(['N','H','R']):
  y=next(y for y in a if y['id']==x['id'] and y['arm']==arm);acts=' '.join(sym[t] for t in y['actions'].split('/') if t)
  ax.add_patch(Rectangle((j-.48,r-.45),.96,.90,facecolor=colors[y['reason']],alpha=.96));ax.text(j,r,acts,ha='center',va='center',fontsize=7,color='white',weight='bold',family='DejaVu Sans Mono')
ax.set(xticks=[0,1,2],xticklabels=['Neutral','Reminder','Return'],yticks=range(len(ordered)),yticklabels=[x['id']+'   '+sym[x['action']] for x in ordered],xlim=(-.53,2.53),ylim=(len(ordered)-.5,-.7))
ax.axhline(11.5,color='#263846',lw=1);ax.xaxis.tick_top();ax.tick_params(length=0,pad=3)
for spine in ax.spines.values():spine.set_visible(False)
ax=axs[1];ax.axis('off');ax.set_title('b   Matched outcomes',loc='left',fontweight='bold',fontsize=9)
ax.text(0,.92,'Accepted / recoverable prefixes',fontweight='bold',transform=ax.transAxes)
for j,arm in enumerate(['N','H','R']):
 sub=[x for x in a if x['arm']==arm and x['access']=='True'];n=sum(x['accepted']=='True' for x in sub);yy=.84-j*.10
 ax.add_patch(Rectangle((0,yy-.03),.62,.032,transform=ax.transAxes,color='#e4ecea'));ax.add_patch(Rectangle((0,yy-.03),.62*n/len(sub),.032,transform=ax.transAxes,color=colors['accepted']))
 ax.text(0,yy+.01,dict(N='Neutral',H='Reminder',R='Return')[arm],transform=ax.transAxes,fontsize=8);ax.text(.67,yy-.018,f'{n} / {len(sub)}',transform=ax.transAxes,weight='bold')
ax.text(0,.50,'Terminal state',fontweight='bold',transform=ax.transAxes)
for j,(key,lab) in enumerate([('accepted','Independent acceptance'),('escalate','Escalated'),('defer','Wait with condition'),('horizon','Decision limit')]):
 yy=.45-j*.047;ax.add_patch(Rectangle((0,yy-.017),.047,.03,transform=ax.transAxes,color=colors[key]));ax.text(.08,yy,lab,va='center',transform=ax.transAxes)
ax.text(0,.25,'Action sequence',fontweight='bold',transform=ax.transAxes);ax.text(0,.218,'D  dispatch     Q  inspect\nA  accept         E  escalate\nW  defer           !  invalid format',va='top',transform=ax.transAxes,linespacing=1.6)
ax.text(0,.005,'Above rule: replacement available.\nBelow rule: admin grant required.',transform=ax.transAxes,fontsize=7,color='#33495a')
for ext in ['pdf','png','svg']:fig.savefig(p/('p1-fig7-case-replay.'+ext),dpi=300,bbox_inches='tight')
