# -*- coding: utf-8 -*-
"""Research fixtures. Private oracle vectors are never included in model prompts."""
import json
from pathlib import Path

TASKS={
'p1_intervals':{
'paper':'P1','family':'interval_union','split':'main',
'spec':'Implement solve(data). data is a list of [start,end] integer pairs. Reject any reversed pair with ValueError. Return the sorted union as [start,end] pairs, merging overlapping or touching intervals. Keep isolated zero-length intervals. Empty input returns []. Do not mutate the input.',
'seed':'def solve(data):\n    return sorted(data)\n',
'reference':'''def solve(data):
    parts = sorted([list(x) for x in data])
    if any(a > b for a,b in parts): raise ValueError('reversed')
    out=[]
    for a,b in parts:
        if out and a <= out[-1][1]: out[-1][1]=max(out[-1][1],b)
        else: out.append([a,b])
    return out
''',
'inputs':[[],[[1,3],[2,5]],[[3,4],[1,3]],[[0,0],[2,2]],[[5,8],[-3,-1],[0,2]],[[4,1]],[[1,8],[2,3],[8,9]],[[2,2],[2,2]]]},
'p1_allocate':{
'paper':'P1','family':'integer_apportionment','split':'main',
'spec':'Implement solve(data) for {total: nonnegative integer, weights: list of nonnegative integers}. Allocate exactly total integer units proportionally to weights: take each floor quota then assign remaining units to descending fractional remainder, ties by lower input index. Return allocation list. Negative total or weights raise ValueError. Empty or all-zero weights return a zero list only when total is zero; otherwise raise ValueError. Use exact integer arithmetic, including large integers.',
'seed':"def solve(data):\n    return [0 for _ in data['weights']]\n",
'reference':'''def solve(data):
    t,w=data['total'],data['weights']
    if t<0 or any(x<0 for x in w): raise ValueError('negative')
    s=sum(w)
    if s==0:
        if t: raise ValueError('zero weights')
        return [0]*len(w)
    out=[t*x//s for x in w]
    order=sorted(range(len(w)), key=lambda i:(-(t*w[i]%s),i))
    for i in order[:t-sum(out)]: out[i]+=1
    return out
''',
'inputs':[{'total':0,'weights':[]},{'total':7,'weights':[1,1,1]},{'total':5,'weights':[1,2]},{'total':1,'weights':[0,2,2]},{'total':2,'weights':[0,0]},{'total':-1,'weights':[2]},{'total':3,'weights':[-1,2]},{'total':100000000000000001,'weights':[1,1,1]},{'total':0,'weights':[0,0]}]},
'p1_order':{
'paper':'P1','family':'dependency_order','split':'main',
'spec':'Implement solve(data) for {nodes: list of distinct strings, edges: list of [before,after] pairs}. Return a topological ordering, choosing the lexicographically smallest currently available node at every step. Duplicate edges count once. Unknown edge endpoints, self cycles or any cycle raise ValueError. Empty graph returns []. Isolated nodes must be included. Do not mutate input.',
'seed':"def solve(data):\n    return sorted(data['nodes'])\n",
'reference':'''def solve(data):
    nodes=set(data['nodes']); edges=set(tuple(x) for x in data['edges'])
    if any(a not in nodes or b not in nodes for a,b in edges): raise ValueError('unknown')
    out=[]
    while nodes:
        avail=sorted(x for x in nodes if not any(b==x and a in nodes for a,b in edges))
        if not avail: raise ValueError('cycle')
        n=avail[0];out.append(n);nodes.remove(n)
    return out
''',
'inputs':[{'nodes':[],'edges':[]},{'nodes':['a','b','c'],'edges':[['c','a']]},{'nodes':['d','b','a','c'],'edges':[['d','b'],['d','a']]},{'nodes':['a','b'],'edges':[['a','b'],['a','b']]},{'nodes':['a','b'],'edges':[['a','b'],['b','a']]},{'nodes':['a'],'edges':[['x','a']]},{'nodes':['a'],'edges':[['a','a']]},{'nodes':['x','z','y'],'edges':[]}]},
'p2_records':{
'paper':'P2','family':'record_aggregation','split':'main',
'spec':'Implement solve(data). data is a list of {name: string, quantity: integer}. Normalize names with strip().casefold(); skip empty normalized names. Negative quantity raises ValueError, including on skipped names. Aggregate quantities by normalized name and return sorted [{name: normalized_name, quantity: total}]. Preserve zero totals. Empty list returns []. Do not mutate input.',
'seed':"def solve(data):\n    return data\n",
'reference':'''def solve(data):
    sums={}
    for row in data:
        if row['quantity']<0: raise ValueError('negative')
        k=row['name'].strip().casefold()
        if k: sums[k]=sums.get(k,0)+row['quantity']
    return [{'name':k,'quantity':sums[k]} for k in sorted(sums)]
''',
'inputs':[[],[{'name':' A ','quantity':2},{'name':'a','quantity':3}],[{'name':'','quantity':2}],[{'name':' Z','quantity':0},{'name':'b','quantity':1}],[{'name':'Straße','quantity':2},{'name':'STRASSE','quantity':3}],[{'name':'x','quantity':-1}],[{'name':' ','quantity':-1}],[{'name':'x','quantity':0}]]},
'p2_expiry':{
'paper':'P2','family':'time_window_filter','split':'main',
'spec':'Implement solve(data) for {now: integer, records: list of {id: distinct string, created: integer, ttl: integer}}. A record is active exactly when created <= now < created+ttl. Any negative ttl raises ValueError, even for future records. Return active IDs sorted lexicographically. Zero ttl is never active. Empty records returns []. Do not mutate input.',
'seed':"def solve(data):\n    return sorted(x['id'] for x in data['records'])\n",
'reference':'''def solve(data):
    rows=data['records'];now=data['now']
    if any(r['ttl']<0 for r in rows): raise ValueError('negative ttl')
    return sorted(r['id'] for r in rows if r['created']<=now<r['created']+r['ttl'])
''',
'inputs':[{'now':0,'records':[]},{'now':10,'records':[{'id':'a','created':5,'ttl':5}]},{'now':5,'records':[{'id':'z','created':5,'ttl':1},{'id':'a','created':1,'ttl':9}]},{'now':0,'records':[{'id':'a','created':1,'ttl':2}]},{'now':5,'records':[{'id':'a','created':5,'ttl':0}]},{'now':0,'records':[{'id':'a','created':9,'ttl':-1}]},{'now':-2,'records':[{'id':'z','created':-5,'ttl':4}]},{'now':10**18,'records':[{'id':'a','created':10**18-1,'ttl':2}]}]},
'p2_rank':{
'paper':'P2','family':'stable_ranking','split':'main',
'spec':'Implement solve(data) for {k: integer, entries: list of {name: string, score: integer, active: boolean}}. Ignore inactive entries; for duplicate active names keep their maximum score. Return up to k names by descending score, breaking ties lexicographically. Negative k raises ValueError. k=0 or no active entries returns []. Do not mutate input.',
'seed':"def solve(data):\n    return [x['name'] for x in data['entries']][:data['k']]\n",
'reference':'''def solve(data):
    if data['k']<0: raise ValueError('negative k')
    scores={}
    for row in data['entries']:
        if row['active']:
            n=row['name'];scores[n]=max(scores.get(n,row['score']),row['score'])
    return sorted(scores,key=lambda n:(-scores[n],n))[:data['k']]
''',
'inputs':[{'k':2,'entries':[]},{'k':2,'entries':[{'name':'z','score':2,'active':True},{'name':'a','score':2,'active':True}]},{'k':2,'entries':[{'name':'a','score':0,'active':True},{'name':'a','score':3,'active':True},{'name':'b','score':2,'active':True}]},{'k':1,'entries':[{'name':'a','score':9,'active':False},{'name':'b','score':1,'active':True}]},{'k':0,'entries':[{'name':'x','score':1,'active':True}]},{'k':-1,'entries':[]},{'k':3,'entries':[{'name':'z','score':-2,'active':True},{'name':'a','score':-1,'active':True}]},{'k':4,'entries':[{'name':'x','score':0,'active':False}]}]},
'dev_abs':{
'paper':'P1','family':'development_abs','split':'development',
'spec':'Implement solve(data) for a list of integers: return their absolute values in original order. Empty input returns []. Do not mutate input.',
'seed':'def solve(data):\n    return data\n',
'reference':'def solve(data):\n    return [abs(x) for x in data]\n',
'inputs':[[],[-2,0,3],[-9],[1,2,3]]},
'dev_reverse':{
'paper':'P2','family':'development_reverse','split':'development',
'spec':'Implement solve(data) for a list of integers: return it in reverse order. Empty input returns []. Do not mutate input.',
'seed':'def solve(data):\n    return data\n',
'reference':'def solve(data):\n    return list(reversed(data))\n',
'inputs':[[],[1,2,3],[0],[2,2,3]]}
}

def expected(task_id,value):
    scope={};exec(TASKS[task_id]['reference'],scope)
    try:return {'output':scope['solve'](json.loads(json.dumps(value)))}
    except Exception as e:return {'error':type(e).__name__}

def vectors(task_id):
    return [{'input':x,**expected(task_id,x)} for x in TASKS[task_id]['inputs']]

if __name__=='__main__':
    Path(__file__).with_name('tasks-private.json').write_text(json.dumps({k:{**v,'vectors':vectors(k)} for k,v in TASKS.items()},ensure_ascii=False,indent=2)+'\n')
