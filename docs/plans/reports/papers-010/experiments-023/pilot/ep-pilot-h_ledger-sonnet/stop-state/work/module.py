def solve(data):
    balance = 0
    holds = {}
    held = 0
    for o in data['ops']:
        kind = o.get('kind') if isinstance(o, dict) else None
        if kind not in ('credit', 'debit', 'hold', 'release'):
            raise ValueError('bad kind')
        amount = o.get('amount')
        if kind != 'release':
            if isinstance(amount, bool) or not isinstance(amount, int) or amount < 1:
                raise ValueError('bad amount')
        if kind == 'hold':
            if o.get('id') in holds:
                raise ValueError('duplicate hold')
        elif kind == 'release':
            if o.get('id') not in holds:
                raise ValueError('unknown hold')
        if kind in ('debit', 'hold'):
            if amount > balance - held:
                raise ValueError('insufficient funds')
        if kind == 'credit':
            balance += amount
        elif kind == 'debit':
            balance -= amount
        elif kind == 'hold':
            holds[o.get('id')] = amount
            held += amount
        else:
            held -= holds.pop(o.get('id'))
    return {'balance': balance, 'held': held, 'available': balance - held}
