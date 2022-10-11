# -*- Mode:Python; indent-tabs-mode:nil; tab-width:4 -*-
#
# Copyright 2022 Canonical Ltd.
#
# This program is free software; you can redistribute it and/or
# modify it under the terms of the GNU Lesser General Public
# License version 3 as published by the Free Software Foundation.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
# Lesser General Public License for more details.
#
# You should have received a copy of the GNU Lesser General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.

import sys

from .engine import engine


def auto_connections_op(
    target_snap, context_snaps, interface, candidates, model, store, classic, f
):
    "simulate auto-connections"
    to_consider = set(context_snaps) | {target_snap}
    # prepare
    for name in to_consider:
        f.snap_ids(name)
    brand, model = model.split("/", 2)
    params = {
        "classic": classic,
        "brand": brand,
        "model": model,
        "target-snap": target_snap,
        "snaps": context_snaps,
    }
    if store:
        params["store"] = store
    out = engine("auto-connections", **params)

    if "error" in out:
        print(f'simulation: {out["error"]}', file=sys.stderr)
        sys.exit(1)

    # installing issues
    installing = {}
    for inst in out["installing"]:
        installing[inst["snap-name"]] = inst
    seen = set()

    def prinst(name):
        if name in seen:
            return
        seen.add(name)
        inst = installing[name]
        inst_res = "OK"
        if inst["error"] != "":
            inst_res = inst["error"]
        print(f"installing {name}: {inst_res}")
        badifaces = inst.get("bad-interfaces")
        if badifaces:
            print(f"  bad-interfaces: {badifaces}")

    for name in context_snaps:
        if name == target_snap:
            continue
        prinst(name)
    prinst(target_snap)

    # connections
    conns = out["connections"]
    if conns is None:
        conns = []

    def conn_key(conn):
        on_target = set(conn["on-target"])
        if on_target == {"plug", "slot"}:
            return (0, conn["plug"]["snap"], conn["plug"]["plug"], conn["slot"]["slot"])
        elif on_target == {"slot"}:
            return (1, conn["plug"]["snap"], conn["plug"]["plug"], conn["slot"]["slot"])
        else:  # plug
            return (2, conn["slot"]["snap"], conn["slot"]["slot"], conn["plug"]["plug"])

    conns.sort(key=conn_key)
    connected_plugs = set()
    connected_slots = set()

    def relevant(x):
        if interface is None:
            return True
        return x["interface"] == interface

    def prconn(conn):
        if not relevant(conn):
            # skip
            return
        if "slot" in conn["on-target"]:
            print(
                f"{conn['plug']['snap']}:{conn['plug']['plug']} < {conn['slot']['slot']}"
            )
            if candidates:
                prcandidates(out, conn["slot"]["slot"], other_side="plug", happy=True)
            connected_slots.add(conn["slot"]["slot"])
        else:
            print(
                f"{conn['slot']['snap']}:{conn['slot']['slot']} > {conn['plug']['plug']}"
            )
            if candidates:
                prcandidates(out, conn["plug"]["plug"], other_side="slot", happy=True)
        if "plug" in conn["on-target"]:
            connected_plugs.add(conn["plug"]["plug"])

    for conn in conns:
        prconn(conn)

    # dangling plugs
    plugs = out["plugs"]
    if plugs is None:
        plugs = []
    plugs.sort(key=lambda p: p["name"])
    for plug in plugs:
        if not relevant(plug):
            continue
        name = plug["name"]
        if name not in connected_plugs:
            print(f": {name}")
            if candidates:
                prcandidates(out, name, other_side="slot", happy=False)


def prcandidates(out, name, other_side, happy):
    cands = out[f"{other_side}-candidates"].get(name, ())
    side = "plug"
    if other_side == "plug":
        side = "slot"
    if happy and len(cands) == 1:
        return
    if not happy and len(cands) == 0:
        return

    def cand_key(cand):
        order = 1
        if cand["check-error"]:
            order = 0
        else:
            if cand["slots-per-plug-any"]:
                order = 2
        return (order, cand[other_side]["snap"], cand[other_side][other_side])

    cands.sort(key=cand_key)
    own_attr = False
    seen = set()
    check_err = None
    for cand in cands:
        if not own_attr:
            label = ilabel(cand, side)
            if label:
                print(f"  {label}")
            own_attr = True
        other = f"{cand[other_side]['snap']}:{cand[other_side][other_side]}"
        if other in seen:
            continue
        seen.add(other)
        other_label = ilabel(cand, other_side)
        print(f"    {other} {other_label}")
        if cand["check-error"]:
            if cand["check-error"] != check_err:
                check_err = cand["check-error"]
                print(f"    => {check_err}")
            else:
                print("    => //")
        else:
            if cand["slots-per-plug-any"]:
                print("    => ok slots-per-plug:*")
            else:
                print("    => ok")


def ilabel(cand, side):
    attrs = cand[f"{side}-static-attrs"]
    iface = cand["interface"]
    label = attrs.get(iface)
    if label:
        return f"{{{iface}: {label}}}"
    return ""
