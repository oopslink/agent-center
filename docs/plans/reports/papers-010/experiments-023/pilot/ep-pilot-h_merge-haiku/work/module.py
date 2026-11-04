def _kind(v):
    if v is None:
        return 'null'
    if isinstance(v, bool):
        return 'boolean'
    if isinstance(v, (int, float)):
        return 'number'
    if isinstance(v, str):
        return 'string'
    if isinstance(v, dict):
        return 'object'
    if isinstance(v, list):
        return 'list'
    raise ValueError('unsupported value type')


def _copy(v):
    if isinstance(v, dict):
        return {k: _copy(x) for k, x in v.items()}
    if isinstance(v, list):
        return [_copy(x) for x in v]
    return v


def _merge(base, patch):
    out = {k: _copy(v) for k, v in base.items()}
    for k, pv in patch.items():
        if pv is None:
            out.pop(k, None)
        elif k in out:
            out[k] = _merge_value(out[k], pv)
        else:
            out[k] = _copy(pv)
    return out


def _merge_value(bv, pv):
    kb = _kind(bv)
    kp = _kind(pv)
    if kb != kp:
        raise ValueError('type mismatch')
    if kb == 'object':
        return _merge(bv, pv)
    return _copy(pv)


def solve(data):
    if not isinstance(data, dict) or 'base' not in data or 'patch' not in data:
        raise ValueError('input must be an object with base and patch')
    base = data['base']
    patch = data['patch']
    if not isinstance(base, dict) or not isinstance(patch, dict):
        raise ValueError('base and patch must be objects')
    return _merge(base, patch)
