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

import click
from ops import (
    Fetcher,
    auto_connections_op,
    fetch_op,
    snap_at_rev,
)


class SnapAtRevType(click.ParamType):
    name = "snap@rev"

    def convert(self, value, param, ctx):
        if isinstance(value, snap_at_rev):
            return value

        parts = value.rsplit("@", 1)
        if len(parts) == 1:
            return snap_at_rev(value)

        try:
            revno = int(parts[1])
            if revno <= 0:
                raise ValueError
            return snap_at_rev(parts[0], revno)
        except ValueError:
            self.fail(f"{parts[1]!r} in {value!r} is not a valid revision", param, ctx)


SNAP_AT_REV = SnapAtRevType()


@click.group()
def cli():
    pass


@cli.command(short_help=fetch_op.__doc__, help=fetch_op.__doc__)
@click.option("--meta/--no-meta", default=True)
@click.option("--decls/--no-decls", default=True)
@click.argument(
    "snaps", nargs=-1, type=SNAP_AT_REV, required=True, metavar="<snap>[@<rev>]..."
)
def fetch(snaps, meta, decls):
    f = Fetcher()
    fetch_op(snaps, meta=meta, decls=decls, f=f)


@cli.command(short_help=auto_connections_op.__doc__, help=auto_connections_op.__doc__)
@click.option("--model", type=str, default="brand/model", metavar="<brand>/<model>")
@click.option("--store", type=str, default=None, metavar="<store-id>")
@click.argument("target-snap", type=str, required=True, metavar="<target-snap>")
@click.argument("context-snaps", type=str, nargs=-1, metavar="<context-snap>...")
def auto_connections(target_snap, context_snaps, model, store):
    f = Fetcher()
    auto_connections_op(target_snap, context_snaps, model=model, store=store, f=f)


if __name__ == "__main__":
    cli()
