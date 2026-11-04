def _kind(v):
    if isinstance(v, dict):
        return 'object'
    if isinstance(v, list):
        return 'list'
    if isinstance(v, str):
        return 'string'
    if isinstance(v, bool):
        return 'boolean'
    if isinstance(v, (int, float)):
        return 'number'
    if v is None:
        return 'null'
    raise ValueError('unsupported JSON value type')


def _clone(v):
    if isinstance(v, dict):
        return {k: _clone(x) for k, x in v.items()}
    if isinstance(v, list):
        return [_clone(x) for x in v]
    return v


def _merge(base, patch):
    out = _clone(base)
    for k, pv in patch.items():
        if pv is None:
            out.pop(k, None)
            continue
        if k not in out:
            out[k] = _clone(pv)
            continue
        bv = out[k]
        bk = _kind(bv)
        pk = _kind(pv)
        if bk == 'object' and pk == 'object':
            out[k] = _merge(bv, pv)
        elif bk == 'list' and pk == 'list':
            out[k] = _clone(pv)
        elif bk != pk:
            raise ValueError('type mismatch for key %r' % (k,))
        else:
            out[k] = pv
    return out


def solve(data):
    if not isinstance(data, dict):
        raise ValueError('data must be a dict')
    if 'base' not in data or 'patch' not in data:
        raise ValueError('data must contain base and patch')
    base = data['base']
    patch = data['patch']
    if not isinstance(base, dict) or not isinstance(patch, dict):
        raise ValueError('base and patch must be JSON objects')
    return _merge(base, patch)
