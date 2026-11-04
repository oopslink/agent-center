import copy


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
    # target is a private deep copy owned by this call; patch is never mutated.
    for key, pv in patch.items():
        if pv is None:
            target.pop(key, None)
            continue
        if key not in target:
            target[key] = copy.deepcopy(pv)
            continue
        bv = target[key]
        bt = _json_type(bv)
        pt = _json_type(pv)
        if bt != pt:
            raise ValueError('type mismatch for key ' + repr(key))
        if bt == 'object':
            target[key] = _merge(bv, pv)
        else:
            target[key] = copy.deepcopy(pv)
    return target


def solve(data):
    if not isinstance(data, dict) or 'base' not in data or 'patch' not in data:
        raise ValueError('data must be an object with base and patch')
    base = data['base']
    patch = data['patch']
    if not isinstance(base, dict) or not isinstance(patch, dict):
        raise ValueError('base and patch must be objects')
    return _merge(copy.deepcopy(base), patch)
