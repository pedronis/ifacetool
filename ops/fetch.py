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

import os
import json
from collections import namedtuple

from craft_store import UbuntuOneStoreClient, endpoints

from .engine import engine


class Fetcher:
    def __init__(self):
        self.c = UbuntuOneStoreClient(
            base_url="https://dashboard.snapcraft.io",
            storage_base_url="https://upload.apps.staging.ubuntu.com",
            auth_url="https://login.ubuntu.com",
            endpoints=endpoints.U1_SNAP_STORE,
            application_name="snapcraft",  # reuse login with snapcraft
            environment_auth="SNAPCRAFT_STORE_CREDENTIALS",
            user_agent="ifacetool",
        )

    def snap_ids(self, name):
        info_fn = f"{name}/.snap.json"
        if os.path.isfile(info_fn):
            with open(info_fn) as info_f:
                info = json.load(info_f)
        else:
            rsp = self.c.request(
                "GET", f"https://dashboard.snapcraft.io/dev/api/snaps/info/{name}"
            )
            sto_info = rsp.json()
            info = {
                "snap-name": name,
                "snap-id": sto_info["snap_id"],
                "publisher-id": sto_info["publisher"]["id"],
            }
            os.makedirs(name, exist_ok=True)
            with open(info_fn, "w") as info_f:
                info_f.write(json.dumps(info, indent=2))
                info_f.write("\n")
        return info["snap-id"], info["publisher-id"]

    def fetch_metadata(self, snap_at_rev):
        name, revno = snap_at_rev
        if revno is None:
            revno = "latest"
        rsp = self.c.request(
            "GET",
            f"https://dashboard.snapcraft.io/api/v2/snaps/{name}/revisions/{revno}?include-yaml=1",
        )
        rev_data = rsp.json()["revision"]
        return rev_data["revision"], rev_data["snap-yaml"]


snap_at_rev = namedtuple("snap_at_rev", ["name", "revision"], defaults=[None])


def fetch_op(snaps, *, f, meta=True, decls=True):
    "fetch snap metadata and snap-declaration content"
    snap_names = []
    for snap in snaps:
        # creates dir <name> and caches values in <name>/.snap.json
        f.snap_ids(snap.name)
        snap_names.append(snap.name)

        if meta:
            revision, snap_yaml = f.fetch_metadata(snap)
            with open(f"{snap.name}/snap.yaml", "w") as mf:
                mf.write(snap_yaml)
            with open(f"{snap.name}/revision", "w") as rf:
                rf.write(f"{revision}\n")

    if decls:
        engine("fetch-decls", snaps=snap_names)
