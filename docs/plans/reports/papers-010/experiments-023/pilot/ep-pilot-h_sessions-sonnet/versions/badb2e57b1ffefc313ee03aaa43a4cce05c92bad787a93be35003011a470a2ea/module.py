def solve(data):
    gap = data['gap']
    if gap < 0:
        raise ValueError('gap must be non-negative')
    by_user = {}
    for e in data['events']:
        by_user.setdefault(e['user'], []).append(e['t'])
    out = []
    for user in sorted(by_user):
        ts = sorted(by_user[user])
        start = end = ts[0]
        count = 1
        for t in ts[1:]:
            if t - end > gap:
                out.append({'user': user, 'start': start, 'end': end, 'count': count})
                start = end = t
                count = 1
            else:
                end = t
                count += 1
        out.append({'user': user, 'start': start, 'end': end, 'count': count})
    return out
