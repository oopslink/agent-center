from pathlib import Path
import csv
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.patches import Patch
from matplotlib.lines import Line2D
A=Path(__file__).resolve().parent
rows=list(csv.DictReader((A/'p2-pairs.csv').open()));rows.sort(key=lambda r:(r['model'],r['task'],r['binding']))
plt.rcParams.update({'font.family':'DejaVu Sans','font.size':8.5,'pdf.fonttype':42,'svg.fonttype':'none'})
fig,ax=plt.subplots(figsize=(6.95,3.85),layout='constrained');ax.set_axisbelow(True);ax.grid(axis='x',color='#dce3e5',linewidth=.6)
labels=[]
for j,r in enumerate(rows):
 c,w=map(int,(r['controller_turns'],r['worker_decisions']))
 ax.barh(j,c,color='#326877',height=.58);ax.barh(j,w,left=c,color='#cfa966',height=.58)
 ax.text(c+w+.3,j,str(c+w),va='center',color='#2b3e44',fontsize=8)
 if r['both_delivered']=='False':ax.plot(c+w+1.7,j,marker='D',color='#983e37',markersize=4.8)
 labels.append(('Haiku' if 'haiku' in r['model'] else 'Sonnet')+'   '+r['task'].split('_',1)[-1]+' / '+r['binding'])
ax.axhline(5.5,color='#868e93',lw=.65);ax.set_yticks(range(len(rows)),labels);ax.invert_yaxis();ax.set_xlim(0,22);ax.set_xticks(range(0,21,4));ax.tick_params(length=0);ax.set_xlabel('Additional model decisions in the four-role workflow')
for sp in ax.spines.values():sp.set_visible(False)
fig.legend(handles=[Patch(color='#326877',label='Controller'),Patch(color='#cfa966',label='Workers'),Line2D([],[],marker='D',ls='none',color='#983e37',label='Delivery stalled')],ncol=3,loc='outside upper center',frameon=False)
for ext in ['png','pdf','svg']:fig.savefig(A/('p2-fig7-paired-decisions.'+ext),dpi=300)
