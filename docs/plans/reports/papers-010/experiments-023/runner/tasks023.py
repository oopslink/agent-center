# -*- coding: utf-8 -*-
"""023 批次的任务家族：规则显式但相互作用，失败空间来自规则交互而非规格含糊。

设计约束：
- 全部规则写在 spec 里，没有需要猜的东西；难度来自多条规则同时成立时的优先级与边界。
- 前三个向量（公开）覆盖主干路径，其余（隐藏）打在规则交互上。
- `seed` 是研究者置入的错误起点，必须真的过不了公开向量。
- `expected` 由 reference 推导，因此 **reference 错了就会静默定义错误的真值**。
  每个任务另附手写的 `hand_checks`，由 `selfcheck.py` 断言 reference 与手算一致——
  这是对 oracle 的第二来源，不是重复。

split: development 用于定难度与试跑；holdout 只在正式批次使用。
"""

TASKS023 = {

# ---------------------------------------------------------------- 开发集 1
'h_ledger': {
'paper': 'P2', 'family': 'ledger_replay', 'split': 'development',
'spec': (
 'Implement solve(data) for {"ops": list of operations}. Each operation is an object with '
 '"kind" and, depending on kind, "amount" and/or "id". Apply the operations in order and '
 'return {"balance": int, "held": int, "available": int} where available is balance minus held. '
 'Rules, checked in exactly this order for each operation: '
 '(1) "kind" must be one of credit, debit, hold, release; anything else raises ValueError. '
 '(2) For credit, debit and hold, "amount" must be an integer of at least 1; otherwise ValueError. '
 'Booleans are not integers here and raise ValueError. '
 '(3) For hold, "id" must not already name an active hold; for release, "id" must name an active '
 'hold; a violation raises ValueError. '
 '(4) For debit and hold, "amount" must not exceed the currently available funds; otherwise ValueError. '
 'credit increases balance. debit decreases balance. hold adds an active hold of amount under id. '
 'release removes the hold named by id and ignores any "amount" field. The first failing operation '
 'raises and no result is returned. An empty operation list returns zeros. Do not mutate the input.'),
'seed': "def solve(data):\n    b=sum(o.get('amount',0) for o in data['ops'])\n    return {'balance':b,'held':0,'available':b}\n",
'reference': '''def solve(data):
    balance=0;holds={}
    for op in data['ops']:
        kind=op.get('kind')
        if kind not in ('credit','debit','hold','release'):
            raise ValueError('kind')
        if kind in ('credit','debit','hold'):
            amount=op.get('amount')
            if isinstance(amount,bool) or not isinstance(amount,int) or amount<1:
                raise ValueError('amount')
        if kind=='hold' and op.get('id') in holds:
            raise ValueError('duplicate hold')
        if kind=='release' and op.get('id') not in holds:
            raise ValueError('unknown hold')
        available=balance-sum(holds.values())
        if kind in ('debit','hold') and op['amount']>available:
            raise ValueError('insufficient')
        if kind=='credit':balance+=op['amount']
        elif kind=='debit':balance-=op['amount']
        elif kind=='hold':holds[op['id']]=op['amount']
        else:holds.pop(op['id'])
    held=sum(holds.values())
    return {'balance':balance,'held':held,'available':balance-held}
''',
'inputs': [
 {'ops': []},
 {'ops': [{'kind': 'credit', 'amount': 100}, {'kind': 'debit', 'amount': 40}]},
 {'ops': [{'kind': 'credit', 'amount': 50}, {'kind': 'hold', 'amount': 20, 'id': 'h1'}]},
 # 隐藏：release 之后额度恢复
 {'ops': [{'kind': 'credit', 'amount': 50}, {'kind': 'hold', 'amount': 50, 'id': 'h1'},
          {'kind': 'release', 'id': 'h1'}, {'kind': 'debit', 'amount': 50}]},
 # 隐藏：hold 占用的额度不能再被 debit
 {'ops': [{'kind': 'credit', 'amount': 50}, {'kind': 'hold', 'amount': 30, 'id': 'h1'},
          {'kind': 'debit', 'amount': 21}]},
 # 隐藏：优先级——amount 非法先于 id 未知
 {'ops': [{'kind': 'release', 'amount': 0, 'id': 'nope'}]},
 # 隐藏：release 不校验 amount，故上一条的 amount 不触发；这里 id 未知才触发
 {'ops': [{'kind': 'release', 'id': 'nope'}]},
 # 隐藏：优先级——重复 hold 先于额度不足
 {'ops': [{'kind': 'credit', 'amount': 10}, {'kind': 'hold', 'amount': 5, 'id': 'h1'},
          {'kind': 'hold', 'amount': 999, 'id': 'h1'}]},
 # 隐藏：布尔不算整数
 {'ops': [{'kind': 'credit', 'amount': True}]},
 # 隐藏：未知 kind 先于一切
 {'ops': [{'kind': 'freeze', 'amount': -1}]},
],
'hand_checks': [
 ({'ops': []}, {'output': {'balance': 0, 'held': 0, 'available': 0}}),
 ({'ops': [{'kind': 'credit', 'amount': 100}, {'kind': 'debit', 'amount': 40}]},
  {'output': {'balance': 60, 'held': 0, 'available': 60}}),
 ({'ops': [{'kind': 'credit', 'amount': 50}, {'kind': 'hold', 'amount': 20, 'id': 'h1'}]},
  {'output': {'balance': 50, 'held': 20, 'available': 30}}),
 ({'ops': [{'kind': 'credit', 'amount': 50}, {'kind': 'hold', 'amount': 50, 'id': 'h1'},
           {'kind': 'release', 'id': 'h1'}, {'kind': 'debit', 'amount': 50}]},
  {'output': {'balance': 0, 'held': 0, 'available': 0}}),
 ({'ops': [{'kind': 'credit', 'amount': 50}, {'kind': 'hold', 'amount': 30, 'id': 'h1'},
           {'kind': 'debit', 'amount': 21}]}, {'error': 'ValueError'}),
 ({'ops': [{'kind': 'release', 'amount': 0, 'id': 'nope'}]}, {'error': 'ValueError'}),
 ({'ops': [{'kind': 'credit', 'amount': True}]}, {'error': 'ValueError'}),
 ({'ops': [{'kind': 'freeze', 'amount': -1}]}, {'error': 'ValueError'}),
]},

# ---------------------------------------------------------------- 开发集 2
'h_sessions': {
'paper': 'P2', 'family': 'session_windows', 'split': 'development',
'spec': (
 'Implement solve(data) for {"gap": integer, "events": list of {"user": string, "t": integer}}. '
 'A negative gap raises ValueError. Group events by user. Within one user, order events by t '
 'ascending, keeping input order for equal t. A new session begins when the difference between an '
 'event\'s t and the previous event\'s t is strictly greater than gap; a difference equal to gap '
 'stays in the same session. Each session is {"user": user, "start": smallest t, "end": largest t, '
 '"count": number of events in it}, where count includes repeated identical t values. Return all '
 'sessions sorted by user ascending, then by start ascending. An empty event list returns []. '
 'Do not mutate the input.'),
'seed': "def solve(data):\n    return [{'user':e['user'],'start':e['t'],'end':e['t'],'count':1} for e in data['events']]\n",
'reference': '''def solve(data):
    gap=data['gap']
    if gap<0:raise ValueError('gap')
    byuser={}
    for i,e in enumerate(data['events']):
        byuser.setdefault(e['user'],[]).append((e['t'],i))
    out=[]
    for user in sorted(byuser):
        pairs=sorted(byuser[user],key=lambda p:(p[0],p[1]))
        cur=None
        for t,_i in pairs:
            if cur is None or t-cur['end']>gap:
                if cur is not None:out.append(cur)
                cur={'user':user,'start':t,'end':t,'count':1}
            else:
                cur['end']=t;cur['count']+=1
        if cur is not None:out.append(cur)
    return sorted(out,key=lambda s:(s['user'],s['start']))
''',
'inputs': [
 {'gap': 5, 'events': []},
 {'gap': 5, 'events': [{'user': 'a', 't': 1}, {'user': 'a', 't': 3}]},
 {'gap': 2, 'events': [{'user': 'a', 't': 1}, {'user': 'a', 't': 10}]},
 # 隐藏：差值恰等于 gap 不分段
 {'gap': 3, 'events': [{'user': 'a', 't': 0}, {'user': 'a', 't': 3}, {'user': 'a', 't': 7}]},
 # 隐藏：按用户独立分段，且输出按 user 再按 start 排
 {'gap': 1, 'events': [{'user': 'b', 't': 10}, {'user': 'a', 't': 0}, {'user': 'b', 't': 11}]},
 # 隐藏：重复 t 计入 count
 {'gap': 0, 'events': [{'user': 'a', 't': 4}, {'user': 'a', 't': 4}]},
 # 隐藏：gap=0 时相邻不同 t 必分段
 {'gap': 0, 'events': [{'user': 'a', 't': 1}, {'user': 'a', 't': 2}]},
 # 隐藏：乱序输入须先排序
 {'gap': 2, 'events': [{'user': 'a', 't': 9}, {'user': 'a', 't': 1}, {'user': 'a', 't': 10}]},
 # 隐藏：负 gap
 {'gap': -1, 'events': []},
 # 隐藏：负时间戳
 {'gap': 2, 'events': [{'user': 'z', 't': -5}, {'user': 'z', 't': -4}]},
],
'hand_checks': [
 ({'gap': 5, 'events': []}, {'output': []}),
 ({'gap': 5, 'events': [{'user': 'a', 't': 1}, {'user': 'a', 't': 3}]},
  {'output': [{'user': 'a', 'start': 1, 'end': 3, 'count': 2}]}),
 ({'gap': 2, 'events': [{'user': 'a', 't': 1}, {'user': 'a', 't': 10}]},
  {'output': [{'user': 'a', 'start': 1, 'end': 1, 'count': 1},
              {'user': 'a', 'start': 10, 'end': 10, 'count': 1}]}),
 ({'gap': 3, 'events': [{'user': 'a', 't': 0}, {'user': 'a', 't': 3}, {'user': 'a', 't': 7}]},
  {'output': [{'user': 'a', 'start': 0, 'end': 3, 'count': 2},
              {'user': 'a', 'start': 7, 'end': 7, 'count': 1}]}),
 ({'gap': 1, 'events': [{'user': 'b', 't': 10}, {'user': 'a', 't': 0}, {'user': 'b', 't': 11}]},
  {'output': [{'user': 'a', 'start': 0, 'end': 0, 'count': 1},
              {'user': 'b', 'start': 10, 'end': 11, 'count': 2}]}),
 ({'gap': 0, 'events': [{'user': 'a', 't': 4}, {'user': 'a', 't': 4}]},
  {'output': [{'user': 'a', 'start': 4, 'end': 4, 'count': 2}]}),
 ({'gap': 0, 'events': [{'user': 'a', 't': 1}, {'user': 'a', 't': 2}]},
  {'output': [{'user': 'a', 'start': 1, 'end': 1, 'count': 1},
              {'user': 'a', 'start': 2, 'end': 2, 'count': 1}]}),
 ({'gap': -1, 'events': []}, {'error': 'ValueError'}),
]},

# ---------------------------------------------------------------- 开发集 3
'h_merge': {
'paper': 'P2', 'family': 'config_merge', 'split': 'development',
'spec': (
 'Implement solve(data) for {"base": object, "patch": object}. Both must be JSON objects; '
 'otherwise raise ValueError. Produce the deep merge of patch into base. For each key in patch: '
 'if the value is null, remove that key from the result, and removing a key that is absent is a '
 'no-op; if both the base value and the patch value are objects, merge them recursively by the '
 'same rules; if both are lists, the patch list replaces the base list entirely with no '
 'concatenation; if the base key is absent, the patch value is inserted as is; otherwise, when the '
 'two values are of different JSON types, raise ValueError. Two values count as the same type when '
 'both are objects, both are lists, both are strings, both are booleans, or both are numbers; '
 'booleans and numbers are different types. Key order in the result is: the keys of base in their '
 'original order, then the keys found only in patch in their order of appearance in patch. '
 'Do not mutate either input.'),
'seed': "def solve(data):\n    out=dict(data['base'])\n    out.update(data['patch'])\n    return out\n",
'reference': '''def solve(data):
    def kind(v):
        if isinstance(v,bool):return 'bool'
        if isinstance(v,dict):return 'obj'
        if isinstance(v,list):return 'list'
        if isinstance(v,str):return 'str'
        if isinstance(v,(int,float)):return 'num'
        return 'null' if v is None else 'other'
    def merge(b,p):
        if kind(b)!='obj' or kind(p)!='obj':raise ValueError('object required')
        out={}
        for k in b:
            if k in p and p[k] is None:continue
            if k in p:
                if kind(b[k])=='obj' and kind(p[k])=='obj':out[k]=merge(b[k],p[k])
                elif kind(b[k])==kind(p[k]):out[k]=p[k]
                else:raise ValueError('type conflict')
            else:out[k]=b[k]
        for k in p:
            if k in b or p[k] is None:continue
            out[k]=p[k]
        return out
    base,patch=data['base'],data['patch']
    if kind(base)!='obj' or kind(patch)!='obj':raise ValueError('object required')
    return merge(base,patch)
''',
'inputs': [
 {'base': {}, 'patch': {}},
 {'base': {'a': 1}, 'patch': {'b': 2}},
 {'base': {'a': {'x': 1}}, 'patch': {'a': {'y': 2}}},
 # 隐藏：列表整体替换，不拼接
 {'base': {'a': [1, 2]}, 'patch': {'a': [3]}},
 # 隐藏：null 删除已有键
 {'base': {'a': 1, 'b': 2}, 'patch': {'a': None}},
 # 隐藏：null 删除不存在的键是 no-op，且不引入该键
 {'base': {'a': 1}, 'patch': {'zz': None}},
 # 隐藏：键序——base 原序在前，patch 独有键按出现序在后
 {'base': {'b': 1, 'a': 1}, 'patch': {'z': 1, 'c': 1}},
 # 隐藏：类型冲突
 {'base': {'a': 1}, 'patch': {'a': 'one'}},
 # 隐藏：布尔与数字是不同类型
 {'base': {'a': True}, 'patch': {'a': 1}},
 # 隐藏：顶层非对象
 {'base': [], 'patch': {}},
],
'hand_checks': [
 ({'base': {}, 'patch': {}}, {'output': {}}),
 ({'base': {'a': 1}, 'patch': {'b': 2}}, {'output': {'a': 1, 'b': 2}}),
 ({'base': {'a': {'x': 1}}, 'patch': {'a': {'y': 2}}}, {'output': {'a': {'x': 1, 'y': 2}}}),
 ({'base': {'a': [1, 2]}, 'patch': {'a': [3]}}, {'output': {'a': [3]}}),
 ({'base': {'a': 1, 'b': 2}, 'patch': {'a': None}}, {'output': {'b': 2}}),
 ({'base': {'a': 1}, 'patch': {'zz': None}}, {'output': {'a': 1}}),
 ({'base': {'b': 1, 'a': 1}, 'patch': {'z': 1, 'c': 1}},
  {'output': {'b': 1, 'a': 1, 'z': 1, 'c': 1}}),
 ({'base': {'a': 1}, 'patch': {'a': 'one'}}, {'error': 'ValueError'}),
 ({'base': {'a': True}, 'patch': {'a': 1}}, {'error': 'ValueError'}),
 ({'base': [], 'patch': {}}, {'error': 'ValueError'}),
]},

# ---------------------------------------------------------------- 留出集 1
'o_buckets': {
'paper': 'P2', 'family': 'rate_buckets', 'split': 'holdout',
'spec': (
 'Implement solve(data) for {"width": integer, "requests": list of integers}. A width below 1 '
 'raises ValueError. Each request timestamp belongs to the bucket floor(t / width), using floor '
 'division that rounds towards negative infinity for negative timestamps. Return one entry per '
 'non-empty bucket as {"bucket": index, "start": index * width, "count": number of requests}, '
 'sorted by bucket ascending. Repeated identical timestamps each count. An empty request list '
 'returns []. Do not mutate the input.'),
'seed': "def solve(data):\n    w=data['width']\n    return [{'bucket':t//w,'start':t,'count':1} for t in data['requests']]\n",
'reference': '''def solve(data):
    w=data['width']
    if w<1:raise ValueError('width')
    counts={}
    for t in data['requests']:
        b=t//w
        counts[b]=counts.get(b,0)+1
    return [{'bucket':b,'start':b*w,'count':counts[b]} for b in sorted(counts)]
''',
'inputs': [
 {'width': 5, 'requests': []},
 {'width': 5, 'requests': [0, 1, 2]},
 {'width': 5, 'requests': [4, 5]},
 {'width': 5, 'requests': [-1]},
 {'width': 5, 'requests': [-5, -6]},
 {'width': 1, 'requests': [7, 7, 7]},
 {'width': 0, 'requests': []},
 {'width': -2, 'requests': [1]},
 {'width': 3, 'requests': [10, -10]},
 {'width': 4, 'requests': [0, 0, 3, 4]},
],
'hand_checks': [
 ({'width': 5, 'requests': []}, {'output': []}),
 ({'width': 5, 'requests': [0, 1, 2]}, {'output': [{'bucket': 0, 'start': 0, 'count': 3}]}),
 ({'width': 5, 'requests': [4, 5]},
  {'output': [{'bucket': 0, 'start': 0, 'count': 1}, {'bucket': 1, 'start': 5, 'count': 1}]}),
 ({'width': 5, 'requests': [-1]}, {'output': [{'bucket': -1, 'start': -5, 'count': 1}]}),
 ({'width': 5, 'requests': [-5, -6]},
  {'output': [{'bucket': -2, 'start': -10, 'count': 1}, {'bucket': -1, 'start': -5, 'count': 1}]}),
 ({'width': 0, 'requests': []}, {'error': 'ValueError'}),
 ({'width': -2, 'requests': [1]}, {'error': 'ValueError'}),
 ({'width': 4, 'requests': [0, 0, 3, 4]},
  {'output': [{'bucket': 0, 'start': 0, 'count': 3}, {'bucket': 1, 'start': 4, 'count': 1}]}),
]},

# ---------------------------------------------------------------- 留出集 2
'o_paths': {
'paper': 'P2', 'family': 'path_normalize', 'split': 'holdout',
'spec': (
 'Implement solve(data) for a list of POSIX-style path strings. Normalize each path and return the '
 'results in input order. An empty string raises ValueError. A path starting with "/" is absolute; '
 'otherwise it is relative. Collapse repeated slashes, drop "." segments, and resolve each ".." '
 'against the preceding segment. For an absolute path, a ".." that would go above the root raises '
 'ValueError. For a relative path, leading ".." segments that cannot be resolved are kept. Drop a '
 'trailing slash, except that the normalized root stays "/". A relative path that normalizes away '
 'entirely becomes ".". Do not mutate the input.'),
'seed': "def solve(data):\n    return [p.replace('//','/') for p in data]\n",
'reference': '''def solve(data):
    out=[]
    for p in data:
        if p=='':raise ValueError('empty')
        absolute=p.startswith('/')
        parts=[]
        for seg in p.split('/'):
            if seg=='' or seg=='.':continue
            if seg=='..':
                if parts and parts[-1]!='..':parts.pop()
                elif absolute:raise ValueError('above root')
                else:parts.append('..')
            else:parts.append(seg)
        if absolute:out.append('/'+'/'.join(parts))
        else:out.append('/'.join(parts) if parts else '.')
    return out
''',
'inputs': [
 ['a/b'],
 ['/a//b/'],
 ['/a/./b/../c'],
 ['/..'],
 ['../a'],
 ['a/../..'],
 ['.'],
 ['/'],
 [''],
 ['a/b/../../c/./d//'],
],
'hand_checks': [
 (['a/b'], {'output': ['a/b']}),
 (['/a//b/'], {'output': ['/a/b']}),
 (['/a/./b/../c'], {'output': ['/a/c']}),
 (['/..'], {'error': 'ValueError'}),
 (['../a'], {'output': ['../a']}),
 (['a/../..'], {'output': ['..']}),
 (['.'], {'output': ['.']}),
 (['/'], {'output': ['/']}),
 ([''], {'error': 'ValueError'}),
 (['a/b/../../c/./d//'], {'output': ['c/d']}),
]},
}
