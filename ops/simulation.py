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

    def prconn(conn):
        if "slot" in conn["on-target"]:
            print(
                f"{conn['plug']['snap']}:{conn['plug']['plug']} < {conn['slot']['slot']}"
            )
            connected_slots.add(conn["slot"]["slot"])
        else:
            print(
                f"{conn['slot']['snap']}:{conn['slot']['slot']} > {conn['plug']['plug']}"
            )
        if "plug" in conn["on-target"]:
            connected_plugs.add(conn["plug"]["plug"])

    for conn in conns:
        prconn(conn)

    # dangling plugs
    plugs = out["plugs"]
    if plugs is None:
        plugs = []
    for plug in plugs:
        name = plug["name"]
        if name not in connected_plugs:
            print(f": {name}")
