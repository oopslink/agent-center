def solve(data):
    gap = data['gap']
    if gap < 0:
        raise ValueError('gap must be non-negative')
    groups = {}
    for e in data['events']:
        groups.setdefault(e['user'], []).append(e['t'])
    result = []
    for user in groups:
        ts = sorted(groups[user])
        start = end = ts[0]
        count = 1
        for t in ts[1:]:
            if t - end > gap:
                result.append({'user': user, 'start': start, 'end': end, 'count': count})
                start = t
                count = 1
            else:
                count += 1
            end = t
        result.append({'user': user, 'start': start, 'end': end, 'count': count})
    result.sort(key=lambda s: (s['user'], s['start']))
    return result
