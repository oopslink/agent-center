from pathlib import Path
import json,datetime as dt,re
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
R=Path(__file__).resolve().parents[1]
d=json.loads((R/'derived/timeline.json').read_text())
def t(s):
 s=s.replace('Z','+00:00');s=re.sub(r'\.(\d+)(?=[+-]\d\d:\d\d$)',lambda m:'.'+m[1][:6].ljust(6,'0'),s);return dt.datetime.fromisoformat(s)
plt.rcParams.update({'font.family':'DejaVu Sans','font.size':9,'axes.spines.top':False,'axes.spines.right':False,'pdf.fonttype':42,'svg.fonttype':'none'})
fig,(ax,bx)=plt.subplots(1,2,figsize=(13.6,5.8),gridspec_kw={'width_ratios':[1.0,1.25]})
colors={'correction':'#A5533D','source-change':'#7B6AA2','positive':'#287C74'};markers={'correction':'o','source-change':'s','positive':'D'}
for i,x in enumerate(d['selected']):
 ax.plot([t('2026-07-20T00:00:00Z'),t('2026-09-06T00:00:00Z')],[i,i],color='#e8e9ed',lw=.8,zorder=0)
 ax.scatter(t(x['time']),i,color=colors[x['type']],marker=markers[x['type']],s=55,zorder=3)
ax.set_yticks(range(7),[x['label'] for x in d['selected']]);ax.set_ylim(6.6,-.65);ax.set_xlim(t('2026-07-20T00:00:00Z'),t('2026-09-07T00:00:00Z'))
ax.set_xticks([t(s+'T00:00:00Z') for s in ['2026-07-22','2026-08-06','2026-08-20','2026-09-04']]);ax.xaxis.set_major_formatter(mdates.DateFormatter('%d %b'));ax.tick_params(axis='y',length=0,labelsize=8.8)
ax.set_title('A   Selected records across the deployment',loc='left',weight='bold',fontsize=11,pad=14);ax.set_xlabel('2026, UTC (gaps do not imply absence)')
for label,typ in [('Human correction','correction'),('Source change','source-change'),('Positive ownership','positive')]:ax.scatter([],[],color=colors[typ],marker=markers[typ],label=label,s=35)
ax.legend(frameon=False,loc='upper left',bbox_to_anchor=(-.9,-.18),ncol=3,columnspacing=1,fontsize=8.5)
colors2={'native':'#2A6F97','user':'#A5533D','message':'#7D838C'};markers2={'native':'s','user':'o','message':'D'}
for i,x in enumerate(d['september']):
 bx.plot([0,x['minutes_since_failed']],[i,i],color='#d7dbe1',lw=1)
 bx.scatter(x['minutes_since_failed'],i,color=colors2[x['type']],marker=markers2[x['type']],s=52,zorder=3)
 bx.text(x['minutes_since_failed']+.13,i,x['label'],va='center',fontsize=8.7)
bx.set_xlim(-.12,15);bx.set_ylim(6.65,-.65);bx.set_yticks(range(7),[t(x['time']).strftime('%H:%M:%S') for x in d['september']]);bx.tick_params(axis='y',length=0,labelsize=8.5)
bx.set_xticks([0,2,4,6]);bx.set_xlabel('Minutes after the failed-state action');bx.set_title('B   4 September: action instruction precedes recovery',loc='left',weight='bold',fontsize=11,pad=14)
for label,typ in [('Native state record','native'),('Human message','user'),('Agent report','message')]:bx.scatter([],[],color=colors2[typ],marker=markers2[typ],label=label,s=35)
bx.legend(frameon=False,loc='upper left',bbox_to_anchor=(-.14,-.18),ncol=3,columnspacing=1,fontsize=8.5)
fig.text(.02,.025,f"By the creation event clock: {d['seconds_action_instruction_to_created_event']:.3f} s after the first human instruction; {d['seconds_created_event_before_identity_reminder']:.3f} s BEFORE the identity reminder.\nIt later failed for a reported baseline mismatch; this figure shows local continuation, not final delivery or a causal improvement curve.",fontsize=8.8,color='#444')
fig.subplots_adjust(left=.21,right=.99,top=.88,bottom=.26,wspace=.4)
out=R/'figures';out.mkdir(exist_ok=True)
for ext in ['png','pdf','svg']:fig.savefig(out/('p1-fig5-responsibility-continuity.'+ext),bbox_inches='tight',dpi=180)
plt.close(fig)
(out/'environment.json').write_text(json.dumps({'matplotlib':matplotlib.__version__},indent=2)+'\n')
print('Figure 5 rendered from selected dated records; no incidence trend inferred.')
