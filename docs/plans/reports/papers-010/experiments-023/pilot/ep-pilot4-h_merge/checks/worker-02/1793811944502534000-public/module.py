def _clone(value):
    # Build a fresh copy of JSON-like data so the caller's inputs are never shared or mutated.
    if isinstance(value, dict):
        return {k: _clone(v) for k, v in value.items()}
    if isinstance(value, list):
        return [_clone(v) for v in value]
    return value


def _json_type(value):
    if value is None:
        return 'null'
    if isinstance(value, bool):
        return 'boolean'
    if isinstance(value, (int, float)):
        return 'number'
    if isinstance(value, str):
        return 'string'
    if isinstance(value, dict):
        return 'object'
    if isinstance(value, list):
        return 'list'
    raise ValueError('unsupported value type')


def _merge(target, patch):
    # target is a private clone owned by this call; patch is read-only.
    for key, pv in patch.items():
        if pv is None:
            target.pop(key, None)
            continue
        if key not in target:
            target[key] = _clone(pv)
            continue
        bv = target[key]
        bt = _json_type(bv)
        pt = _json_type(pv)
        if bt != pt:
            raise ValueError('type mismatch for key ' + repr(key))
        if bt == 'object':
            target[key] = _merge(bv, pv)
        else:
            target[key] = _clone(pv)
    return target


def solve(data):
    if not isinstance(data, dict) or 'base' not in data or 'patch' not in data:
        raise ValueError('data must be an object with base and patch')
    base = data['base']
    patch = data['patch']
    if not isinstance(base, dict) or not isinstance(patch, dict):
        raise ValueError('base and patch must be objects')
    return _merge(_clone(base), patch)
