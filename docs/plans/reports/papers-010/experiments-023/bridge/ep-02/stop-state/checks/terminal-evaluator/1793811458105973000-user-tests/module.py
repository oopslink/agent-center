def solve(data):
    totals = {}
    for entry in data:
        qty = entry["quantity"]
        if qty < 0:
            raise ValueError("negative quantity")
        name = entry["name"].strip().casefold()
        if not name:
            continue
        totals[name] = totals.get(name, 0) + qty
    return [{"name": n, "quantity": totals[n]} for n in sorted(totals)]
