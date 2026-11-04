def solve(data):
    balance = 0
    held = 0
    holds = {}
    for op in data['ops']:
        if not isinstance(op, dict):
            raise ValueError('operation must be an object')
        kind = op.get('kind')
        if kind not in ('credit', 'debit', 'hold', 'release'):
            raise ValueError('invalid kind')
        if kind in ('credit', 'debit', 'hold'):
            amount = op.get('amount')
            if isinstance(amount, bool) or not isinstance(amount, int) or amount < 1:
                raise ValueError('invalid amount')
        if kind == 'credit':
            balance += amount
        elif kind == 'debit':
            if amount > balance - held:
                raise ValueError('insufficient available funds')
            balance -= amount
        elif kind == 'hold':
            op_id = op.get('id')
            if op_id in holds:
                raise ValueError('hold id already active')
            if amount > balance - held:
                raise ValueError('insufficient available funds')
            holds[op_id] = amount
            held += amount
        else:
            op_id = op.get('id')
            if op_id not in holds:
                raise ValueError('no active hold with this id')
            held -= holds.pop(op_id)
    return {'balance': balance, 'held': held, 'available': balance - held}
