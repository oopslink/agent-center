def solve(data):
    balance = 0
    held = 0
    holds = {}
    for op in data['ops']:
        kind = op.get('kind')
        if kind not in ('credit', 'debit', 'hold', 'release'):
            raise ValueError('invalid kind')
        amount = None
        if kind in ('credit', 'debit', 'hold'):
            amount = op.get('amount')
            if isinstance(amount, bool) or not isinstance(amount, int) or amount < 1:
                raise ValueError('invalid amount')
        if kind == 'hold':
            hid = op.get('id')
            if hid in holds:
                raise ValueError('hold id already active')
            if amount > balance - held:
                raise ValueError('insufficient available funds')
            holds[hid] = amount
            held += amount
        elif kind == 'release':
            hid = op.get('id')
            if hid not in holds:
                raise ValueError('no active hold with id')
            held -= holds.pop(hid)
        elif kind == 'credit':
            balance += amount
        else:
            if amount > balance - held:
                raise ValueError('insufficient available funds')
            balance -= amount
    return {'balance': balance, 'held': held, 'available': balance - held}
