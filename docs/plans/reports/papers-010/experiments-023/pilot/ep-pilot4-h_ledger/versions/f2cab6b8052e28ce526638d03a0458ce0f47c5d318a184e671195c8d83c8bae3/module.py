def solve(data):
    b=sum(o.get('amount',0) for o in data['ops'])
    return {'balance':b,'held':0,'available':b}
