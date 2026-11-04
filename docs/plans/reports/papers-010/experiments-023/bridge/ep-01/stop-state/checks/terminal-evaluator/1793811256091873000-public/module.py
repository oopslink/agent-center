def solve(data):
    totals = {}
    for item in data:
        qty = item["quantity"]
        if qty < 0:
            raise ValueError("negative quantity")
        name = item["name"].strip().casefold()
        if not name:
            continue
        totals[name] = totals.get(name, 0) + qty
    return [{"name": n, "quantity": totals[n]} for n in sorted(totals)]
