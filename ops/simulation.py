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
    # XXX use structured json
    # XXX AppArmor blah
    print(out.decode("utf8"), end="")
