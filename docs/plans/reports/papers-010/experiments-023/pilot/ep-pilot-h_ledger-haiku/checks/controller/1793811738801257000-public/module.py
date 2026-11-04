def solve(data):
    ops = data['ops']
    balance = 0
    held = 0
    holds = {}
    for op in ops:
        if not isinstance(op, dict):
            raise ValueError('operation must be an object')
        kind = op.get('kind')
        if kind not in ('credit', 'debit', 'hold', 'release'):
            raise ValueError('unknown kind: %r' % (kind,))
        amount = None
        if kind in ('credit', 'debit', 'hold'):
            amount = op.get('amount')
            if isinstance(amount, bool) or not isinstance(amount, int) or amount < 1:
                raise ValueError('amount must be an integer of at least 1')
        if kind == 'hold':
            oid = op.get('id')
            if oid in holds:
                raise ValueError('hold id already active')
            if amount > balance - held:
                raise ValueError('insufficient available funds')
            holds[oid] = amount
            held += amount
        elif kind == 'release':
            oid = op.get('id')
            if oid not in holds:
                raise ValueError('no active hold with that id')
            held -= holds.pop(oid)
        elif kind == 'debit':
            if amount > balance - held:
                raise ValueError('insufficient available funds')
            balance -= amount
        else:
            balance += amount
    return {'balance': balance, 'held': held, 'available': balance - held}
