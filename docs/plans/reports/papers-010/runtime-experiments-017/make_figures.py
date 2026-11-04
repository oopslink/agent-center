#!/usr/bin/env python3
"""P1/P2 Figure 6: all planned episodes and all valid paired workloads.

Reads only the frozen design/protocol/analyzer and their derived analysis files.
Does not inspect episodes/, select anecdotes, or modify execution/analysis code.
Run after the main batch has been analyzed:
  /Users/oopslink/works/research/agent-harness-study/.venv-figures/bin/python make_figures.py
"""
from pathlib import Path
from collections import Counter
import argparse
import csv
import hashlib
import json
import math

import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
from matplotlib.patches import Rectangle
from matplotlib.lines import Line2D
from matplotlib.transforms import Bbox

ROOT = Path(__file__).resolve().parent
OUT = ROOT/'figures'
MODELS = ['claude-sonnet-5-5', 'claude-haiku-5-5']
MODEL_NAMES = ['Sonnet 5.5', 'Haiku 5.5']
MODEL_COLORS = ['#24647D', '#B66940']
INK = '#202B33'
NEUTRAL = '#F3F5F6'
SUCCESS = '#D7E8EC'
FAILURE = '#F2DFD4'
PAPER_WIDTH_MM = 180
PAPER_HEIGHT_MM = 176
TASKS = {
    'P1': [('p1_allocate','Allocation'), ('p1_intervals','Intervals'), ('p1_order','Dependency')],
    'P2': [('p2_expiry','Expiry'), ('p2_rank','Ranking'), ('p2_records','Records')],
}
CONDITIONS = {
    'P1': [
        ('single:off', 'Single\nreference'),
        ('separate:off', 'Separate\nledger off'),
        ('unified:off', 'Unified\nledger off'),
        ('separate:on', 'Separate\nledger on'),
        ('unified:on', 'Unified\nledger on'),
    ],
    'P2': [
        ('short:distributed', '2 roles\nsplit'),
        ('short:joined', '2 roles\njoined'),
        ('long:distributed', '4 roles\nsplit'),
        ('long:joined', '4 roles\njoined'),
    ],
}
FACTORS = {
    'P1': [
        ('identity', 'unified', 'separate', 'ledger', ['off','on'], 'Unified − separate'),
        ('ledger', 'on', 'off', 'identity', ['separate','unified'], 'Ledger on − off'),
    ],
    'P2': [
        ('organization', 'long', 'short', 'binding', ['distributed','joined'], '4 roles − 2 roles'),
        ('binding', 'joined', 'distributed', 'organization', ['short','long'], 'Joined − split view'),
    ],
}
OTHER_LABEL = {'off':'off','on':'on','separate':'sep.','unified':'unified',
               'distributed':'split','joined':'joined','short':'2 roles','long':'4 roles'}
BOOL_FIELDS = {'valid','autonomous_delivery','independent_pass','implementation_pass',
               'premature_completion','requested_external','reported_blocked',
               'active_worker_at_stop','late_artifact_change','paired_block_valid'}
INT_FIELDS = {'hidden_passed','hidden_total','controller_turns','worker_starts',
              'worker_decisions','model_attempts','transport_retries','format_errors',
              'tool_error_count','worker_failed_versions','controller_spawn_actions',
              'controller_wait_actions','controller_inspect_actions',
              'controller_check_actions','input_tokens','output_tokens',
              'cache_creation_input_tokens','cache_read_input_tokens'}
FLOAT_FIELDS = {'cost_usd_reported','model_duration_s_sum'}
STATUS_KEY = {
    'D':'autonomous delivery', 'PC':'premature complete', 'B':'reported blocked',
    'E':'external request', 'Q':'decision budget exhausted',
    'TI':'transport invalid', 'RI':'runtime invalid', 'HE':'harness exception',
    'IV':'invalid analysis row', 'M':'missing episode', 'O':'other terminal status',
}

plt.rcParams.update({
    'font.family':'DejaVu Sans','font.size':8,'text.color':INK,'axes.labelcolor':INK,
    'axes.titlesize':9,'axes.labelsize':8,'xtick.labelsize':7.2,'ytick.labelsize':7.5,
    'axes.spines.top':False,'axes.spines.right':False,'axes.linewidth':.65,
    'pdf.fonttype':42,'svg.fonttype':'path','svg.hashsalt':'p12-runtime-003',
    'savefig.dpi':300,'figure.dpi':120,
})


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def condition(row):
    if row['paper']=='P1':
        return row['identity']+':'+row['ledger']
    return row['organization']+':'+row['binding']


def load_analysis():
    required = [ROOT/'design.json', ROOT/'protocol.md', ROOT/'analyze.py',
                ROOT/'analysis/episode-results.csv', ROOT/'analysis/condition-summary.json',
                ROOT/'analysis/paired-comparisons.json', ROOT/'analysis/summary.json']
    missing = [str(p) for p in required if not p.is_file()]
    if missing:
        raise RuntimeError('Analysis is not ready; missing files: '+', '.join(missing))
    before = {str(p.relative_to(ROOT)):sha(p) for p in required}
    design = json.loads((ROOT/'design.json').read_text())['runs']
    with (ROOT/'analysis/episode-results.csv').open(newline='') as f:
        rows = list(csv.DictReader(f))
    for row in rows:
        for field,value in list(row.items()):
            if value=='':
                row[field]=None
            elif field in BOOL_FIELDS:
                if value not in {'True','False'}:
                    raise ValueError((row['id'],field,value))
                row[field]=value=='True'
            elif field in INT_FIELDS:
                row[field]=int(value)
            elif field in FLOAT_FIELDS:
                row[field]=float(value)
        # This is a transparent sum of existing decision counts, not a new endpoint.
        c,w=row.get('controller_turns'),row.get('worker_decisions')
        row['total_model_decisions']=None if c is None or w is None else c+w
    summaries=json.loads((ROOT/'analysis/condition-summary.json').read_text())
    pairs=json.loads((ROOT/'analysis/paired-comparisons.json').read_text())
    summary=json.loads((ROOT/'analysis/summary.json').read_text())
    assert len(design)==54 and len(rows)==54
    assert len({r['id'] for r in rows})==54
    assert {r['id'] for r in rows}=={r['id'] for r in design}
    assert summary['planned']==54
    by_id={r['id']:r for r in rows}
    for planned in design:
        row=by_id[planned['id']]
        assert all(row[k]==v for k,v in planned.items()), planned['id']
        if row.get('valid'):
            assert row['controller_turns']>=0 and row['worker_decisions']>=0
            assert row['model_attempts']>=row['total_model_decisions']
    # Independently check the displayed counts against the frozen aggregate file.
    for s in summaries:
        group=[r for r in rows if (r['paper'],r['model'],condition(r))==(s['paper'],s['model'],s['condition'])]
        eligible=[r for r in group if r['valid']]
        assert len(group)==s['planned'] and len(eligible)==s['valid']
        for key in ['autonomous_delivery','independent_pass','implementation_pass',
                    'premature_completion','reported_blocked','requested_external']:
            assert sum(bool(r.get(key)) for r in eligible)==s[key],(s,key)
        assert sum(r['controller_turns'] for r in eligible)==s['controller_turns']['total']
        assert sum(r['model_attempts'] for r in eligible)==s['model_attempts']['total']
    for pair in pairs:
        left,right=by_id[pair['left_id']],by_id[pair['right_id']]
        assert left['paired_block_valid'] and right['paired_block_valid']
        assert left['valid'] and right['valid']
        for metric in ['controller_turns','worker_starts','model_attempts','cost_usd_reported',
                       'autonomous_delivery','independent_pass']:
            d=float(left[metric])-float(right[metric])
            assert math.isclose(d,pair[metric+'_difference'],abs_tol=1e-9)
    after={str(p.relative_to(ROOT)):sha(p) for p in required}
    assert before==after, 'Analysis files changed while reading; rerun after analysis completes.'
    return design,rows,summaries,pairs,summary,[{'path':k,'sha256':v} for k,v in before.items()]


def outcome(row):
    if row['status']=='missing':
        return 'M'
    if not row['valid']:
        return {'transport_invalid':'TI','runtime_invalid':'RI','harness_exception':'HE'}.get(row['status'],'IV')
    if row.get('autonomous_delivery'):
        return 'D'
    if row.get('premature_completion'):
        return 'PC'
    return {'reported_blocked':'B','requested_external':'E','budget_exhausted':'Q'}.get(row['status'],'O')


def episode_matrix(ax,paper,model,rows):
    plotted=[]
    for i,(task,task_label) in enumerate(TASKS[paper]):
        for j,(cond,cond_label) in enumerate(CONDITIONS[paper]):
            selected=[r for r in rows if r['paper']==paper and r['model']==model and r['task']==task and condition(r)==cond]
            assert len(selected)==1,(paper,model,task,cond)
            row=selected[0];code=outcome(row)
            valid=row['valid'];fill=SUCCESS if code=='D' else (FAILURE if valid else NEUTRAL)
            ax.add_patch(Rectangle((j-.49,i-.49),.98,.98,facecolor=fill,edgecolor='white',lw=1.2,
                                   hatch='//' if not valid else None))
            suffix='+' if row.get('independent_pass') and code!='D' else ''
            c,w=row.get('controller_turns'),row.get('worker_decisions')
            count='— / —' if c is None or w is None else f'{c} / {w}'
            ax.text(j,i-.16,code+suffix,ha='center',va='center',fontsize=8.4,weight='bold')
            ax.text(j,i+.20,count,ha='center',va='center',fontsize=7.6)
            plotted.append({'task':task,'condition':cond,'model':model,'episode_id':row['id'],
                            'outcome_code':code,'oracle_pass_without_delivery':suffix=='+',
                            'controller_decisions':c,'worker_decisions':w,
                            'total_model_decisions':row['total_model_decisions'],
                            'model_attempts':row.get('model_attempts'),'status':row['status'],
                            'valid':valid,'paired_block_valid':row['paired_block_valid']})
    ax.set_xlim(-.5,len(CONDITIONS[paper])-.5);ax.set_ylim(2.5,-.5)
    ax.set_xticks(range(len(CONDITIONS[paper])),[s[1] for s in CONDITIONS[paper]],fontsize=6.8)
    ax.xaxis.tick_top();ax.set_yticks(range(3),[s[1] for s in TASKS[paper]],fontsize=7.3)
    ax.tick_params(length=0,pad=4)
    for spine in ax.spines.values():spine.set_visible(False)
    return plotted


def paired_rows(paper,factor_spec,design,rows,pairs):
    factor,a,b,other,other_values,title=factor_spec
    by_id={r['id']:r for r in rows};result=[]
    for task,label in TASKS[paper]:
        for value in other_values:
            for model in MODELS:
                lefts=[r for r in design if r['paper']==paper and r['task']==task and r['model']==model
                       and r.get('identity')!='single' and r[factor]==a and r[other]==value]
                rights=[r for r in design if r['paper']==paper and r['task']==task and r['model']==model
                        and r.get('identity')!='single' and r[factor]==b and r[other]==value]
                assert len(lefts)==len(rights)==1
                left,right=by_id[lefts[0]['id']],by_id[rights[0]['id']]
                matching=[p for p in pairs if p['left_id']==left['id'] and p['right_id']==right['id'] and p['factor']==factor]
                usable=left['paired_block_valid'] and right['paired_block_valid']
                assert len(matching)==int(usable),(paper,factor,task,value,model)
                result.append({'task':task,'task_label':label,'other_factor':other,'other_value':value,
                               'model':model,'factor':factor,'direction':a+' minus '+b,
                               'left_id':left['id'],'right_id':right['id'],'usable':usable,
                               'total_model_decisions_difference':left['total_model_decisions']-right['total_model_decisions'] if usable else None,
                               'controller_decisions_difference':left['controller_turns']-right['controller_turns'] if usable else None,
                               'worker_decisions_difference':left['worker_decisions']-right['worker_decisions'] if usable else None,
                               'model_attempts_difference':matching[0]['model_attempts_difference'] if usable else None,
                               'left_outcome':outcome(left),'right_outcome':outcome(right),
                               'omission_reason':None if usable else 'At least one episode in the task × model block was invalid/missing; frozen paired-block rule.'})
    return result


def paired_plot(ax,entries,paper,spec,bound):
    _,_,_,_,other_values,_=spec
    labels=[]
    for task,label in TASKS[paper]:
        for other in other_values:
            labels.append(f'{label} · {OTHER_LABEL[other]}')
    ax.axvline(0,color='#8C979C',lw=.85,zorder=1)
    for i in [1.5,3.5]:ax.axhline(i,color='#E4E8EA',lw=.6,zorder=0)
    for mi,model in enumerate(MODELS):
        selected=[r for r in entries if r['model']==model]
        assert len(selected)==6
        for i,r in enumerate(selected):
            y=i+(-.13 if mi==0 else .13)
            if not r['usable']:
                ax.text(1.015,y,'NA',transform=ax.get_yaxis_transform(),ha='left',va='center',
                        fontsize=6.5,color=MODEL_COLORS[mi],clip_on=False)
                continue
            x=r['total_model_decisions_difference']
            ax.plot([0,x],[y,y],color=MODEL_COLORS[mi],lw=.7,alpha=.50,zorder=2)
            ax.scatter([x],[y],s=27,marker='o' if mi==0 else '^',facecolor=MODEL_COLORS[mi],
                       edgecolor='white',linewidth=.45,zorder=3)
    ax.set_ylim(5.55,-.55);ax.set_xlim(-bound,bound)
    tick=math.ceil(bound/2)
    ax.set_xticks([-tick,0,tick])
    ax.set_yticks(range(6),labels,fontsize=7.0)
    ax.tick_params(axis='y',length=0,pad=4)
    ax.spines['left'].set_visible(False)
    ax.set_xlabel('Difference in total model decisions',fontsize=7.2,labelpad=6)
    n=[sum(r['usable'] for r in entries if r['model']==m) for m in MODELS]
    return n


def check_bounds(fig):
    fig.canvas.draw();renderer=fig.canvas.get_renderer();bad=[];boxes=[]
    for item in fig.findobj(matplotlib.text.Text):
        if item.get_visible() and item.get_text():
            b=item.get_window_extent(renderer)
            if b.x0<-.5 or b.y0<-.5 or b.x1>fig.bbox.width+.5 or b.y1>fig.bbox.height+.5:
                bad.append({'text':item.get_text(),'bounds':list(b.bounds)})
            boxes.append({'text':item.get_text(),'bounds':list(b.bounds)})
    if bad:raise ValueError({'text_outside_figure':bad})
    return {'all_visible_text_within_canvas':True,'checked_text_objects':len(boxes)}


def save_figure(fig,paper,name):
    bounds=check_bounds(fig)
    outputs=[]
    for ext in ['png','svg','pdf']:
        path=OUT/(name+'.'+ext)
        metadata={'Date':None} if ext=='svg' else ({'CreationDate':None,'ModDate':None} if ext=='pdf' else None)
        fig.savefig(path,metadata=metadata,facecolor='white',dpi=300)
        outputs.append({'path':str(path.relative_to(ROOT)),'sha256':sha(path),'bytes':path.stat().st_size})
    plt.close(fig)
    return {'paper':paper,'figure':6,'name':name,'width_mm':PAPER_WIDTH_MM,
            'height_mm':PAPER_HEIGHT_MM,'png_dpi':300,'font':'DejaVu Sans',
            'bounds_check':bounds,'visual_inspection':'pending','outputs':outputs}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check-only',action='store_true',help='Validate derived data without drawing.')
    args=parser.parse_args()
    design,rows,summary_rows,pairs,summary,provenance=load_analysis()
    if args.check_only:
        print(json.dumps({'planned':len(design),'rows':len(rows),'paired_rows':len(pairs),'source_checks':'passed'}));return
    OUT.mkdir(exist_ok=True)
    data={'source_files':provenance,'script_sha256':sha(Path(__file__)),
          'source_row_ids':[r['id'] for r in rows],'episode_rows':rows,'figures':{},
          'interpretation':{
              'unit':'One episode per task × condition × model; three researcher-constructed task families per paper.',
              'all_cells':'P1 30 episodes including 6 single-session references; P2 24 episodes. Missing and invalid cells retained.',
              'decision_count':'controller_turns + worker_decisions from the frozen analyzer; model decisions, not successful tool calls. Includes malformed model decisions. Scripted seed has no model decisions.',
              'pair_selection':'All frozen valid task × model blocks and both levels of the other factor; no outcome-based selection. Single-session references are not factorial pairs.',
              'pair_direction':'Positive means more recorded model decisions in the first-named condition; zero differences are plotted at zero.',
              'no_population_inference':'No CI or significance test: one episode per cell, only three task families; matched comparisons reuse tasks.',
              'p1_boundary':'Identity wording changes attribution within the same actual topology. Ledger also supplies fresh state; not a pure formatting treatment.',
              'p2_boundary':'2/4 roles count worker functions, excluding the common controller. Organization changes sessions/handoffs jointly.',
          }}
    inventory=[];captions=[]
    for paper in ['P1','P2']:
        fig=plt.figure(figsize=(PAPER_WIDTH_MM/25.4,PAPER_HEIGHT_MM/25.4))
        # Geometry is set independently of observed success or failure outcomes.
        a=fig.add_axes([.13,.66,.36,.22]);b=fig.add_axes([.62,.66,.36,.22])
        cells=[]
        for ax,model,name,x in [(a,MODELS[0],MODEL_NAMES[0],.055),(b,MODELS[1],MODEL_NAMES[1],.545)]:
            cells.extend(episode_matrix(ax,paper,model,rows))
            fig.text(x,.954,('(a) ' if model==MODELS[0] else '(b) ')+name,fontsize=9.2,weight='bold')
        fig.text(.055,.615,'Cells: terminal code; controller / worker model decisions.',fontsize=7.5)
        codes=sorted({c['outcome_code'] for c in cells},key=lambda c:list(STATUS_KEY).index(c))
        parts=[c+' = '+STATUS_KEY[c] for c in codes]
        lines=[];line=''
        for part in parts:
            candidate=line+('   |   ' if line else '')+part
            if len(candidate)>105 and line:lines.append(line);line=part
            else:line=candidate
        if line:lines.append(line)
        if len(lines)>3:raise ValueError('Too many distinct terminal codes for fixed legend; preserve all and enlarge layout explicitly.')
        for i,line in enumerate(lines):fig.text(.055,.592-i*.017,line,fontsize=6.9)
        if any(c['oracle_pass_without_delivery'] for c in cells):
            fig.text(.055,.592-len(lines)*.017,'+ Independent acceptance passed, but autonomous completion was not declared.',fontsize=6.8)
        plotted_pairs=[paired_rows(paper,spec,design,rows,pairs) for spec in FACTORS[paper]]
        observed=[abs(r['total_model_decisions_difference']) for rs in plotted_pairs for r in rs if r['usable']]
        bound=max(4,math.ceil(max(observed,default=0)/4)*4+2)
        ns=[]
        for k,(spec,entries) in enumerate(zip(FACTORS[paper],plotted_pairs)):
            ax=fig.add_axes([.23 if k==0 else .72,.19,.25,.30])
            ns.append(paired_plot(ax,entries,paper,spec,bound))
            fig.text(.055 if k==0 else .545,.522,('(c) ' if k==0 else '(d) ')+spec[-1],fontsize=8.6,weight='bold')
            fig.text(.055 if k==0 else .545,.503,f"Valid pairs: S {ns[-1][0]}/6; H {ns[-1][1]}/6",fontsize=6.9)
        handles=[Line2D([0],[0],marker='o' if i==0 else '^',color=MODEL_COLORS[i],lw=0,
                        markersize=5,label=MODEL_NAMES[i]) for i in range(2)]
        fig.legend(handles=handles,loc='center',bbox_to_anchor=(.5,.086),ncol=2,frameon=False,fontsize=7.6)
        fig.text(.055,.055,'Paired workload = controller + worker decisions; positive means more in the first condition.',fontsize=6.9)
        fig.text(.055,.035,'Three task families per paper; one episode per cell. No population CI or significance test.',fontsize=6.9)
        footer=('Single session is a task-solvability reference, not an isolated topology effect.' if paper=='P1'
                else '2 / 4 roles are worker functions; the common controller is additional in both conditions.')
        fig.text(.055,.015,footer,fontsize=6.9)
        name=('p1-fig6-runtime-identity' if paper=='P1' else 'p2-fig6-runtime-workflow')
        inventory.append(save_figure(fig,paper,name))
        data['figures'][paper]={'name':name,'episode_cells':cells,'paired_workloads':plotted_pairs,
                                'pair_valid_counts_by_factor_model':ns,'paired_x_bound':bound,
                                'status_code_key':{k:STATUS_KEY[k] for k in codes},
                                'all_terminal_statuses':dict(Counter(r['status'] for r in rows if r['paper']==paper)),
                                'sources':'analysis/episode-results.csv; analysis/paired-comparisons.json; design.json',
                                'number_of_planned_cells':len(cells)}
        captions.append(f"## {paper} Figure 6\n\nAll planned multi-turn episodes and paired recorded workload. Panels (a–b) retain every task–condition cell for each fixed model, with terminal code and controller/worker decision counts. Panels (c–d) show each matched task and other-factor level, using the frozen valid-block rule. Positive differences mean more total decisions in the first-named condition. Missing/invalid episodes are distinct from unsuccessful valid outcomes; unavailable pairs appear as NA, not zero. Independent acceptance without autonomous completion is marked '+'. Counts are model decisions, including malformed decisions, not successful tool operations. Three task families and one episode per cell support descriptive comparisons, without population confidence intervals or significance tests.\n\n{footer}\n")
    (ROOT/'figure-data.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
    (ROOT/'figure-inventory.json').write_text(json.dumps(inventory,ensure_ascii=False,indent=2)+'\n')
    (OUT/'captions.md').write_text('\n'.join(captions))
    print(json.dumps({'figures':len(inventory),'outputs':sum(len(x['outputs']) for x in inventory),
                      'all_planned_episode_cells':sum(x['number_of_planned_cells'] for x in data['figures'].values()),
                      'source_checks':'passed','bounds_checks':'passed','visual_inspection':'pending'},ensure_ascii=False))


if __name__=='__main__':
    main()
