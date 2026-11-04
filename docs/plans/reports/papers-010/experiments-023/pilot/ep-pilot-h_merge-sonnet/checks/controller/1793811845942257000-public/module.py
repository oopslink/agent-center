def _t(v):
    if v is None:
        return 'null'
    if isinstance(v, bool):
        return 'bool'
    if isinstance(v, (int, float)):
        return 'number'
    if isinstance(v, str):
        return 'string'
    if isinstance(v, list):
        return 'list'
    if isinstance(v, dict):
        return 'object'
    return 'other'


def _copy(v):
    if isinstance(v, dict):
        return {k: _copy(x) for k, x in v.items()}
    if isinstance(v, list):
        return [_copy(x) for x in v]
    return v


def _merge(base, patch):
    out = {}
    for k, v in base.items():
        out[k] = _copy(v)
    for k, pv in patch.items():
        if pv is None:
            out.pop(k, None)
            continue
        if k not in base:
            out[k] = _copy(pv)
            continue
        bv = base[k]
        tb, tp = _t(bv), _t(pv)
        if tb != tp:
            raise ValueError('type mismatch for key %r' % (k,))
        if tp == 'object':
            out[k] = _merge(bv, pv)
        else:
            out[k] = _copy(pv)
    return out


def solve(data):
    if not isinstance(data, dict):
        raise ValueError('data must be an object')
    if 'base' not in data or 'patch' not in data:
        raise ValueError('missing base or patch')
    base, patch = data['base'], data['patch']
    if not isinstance(base, dict) or not isinstance(patch, dict):
        raise ValueError('base and patch must be objects')
    return _merge(base, patch)
