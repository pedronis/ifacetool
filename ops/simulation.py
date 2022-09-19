from .engine import engine


def auto_connections_op(target_snap, context_snaps, model, store, f):
    "simulate auto-connections"
    to_consider = set(context_snaps) | {target_snap}
    # prepare
    for name in to_consider:
        f.snap_ids(name)
    brand, model = model.split("/", 2)
    params = {
        "brand": brand,
        "model": model,
        "target-snap": target_snap,
        "snaps": context_snaps,
    }
    if store:
        params["store"] = store
    out = engine("auto-connections", **params)
    # XXX use structured json
    # XXX AppArmor blah
    print(out.decode("utf8"), end="")
