def solve(data):
    return [{'user':e['user'],'start':e['t'],'end':e['t'],'count':1} for e in data['events']]
