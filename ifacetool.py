import click
from ops import (
    Fetcher,
    auto_connections_op,
    fetch_op,
)


@click.group()
def cli():
    pass


@cli.command(short_help=fetch_op.__doc__, help=fetch_op.__doc__)
@click.option("--meta/--no-meta", default=True)
@click.option("--decls/--no-decls", default=True)
@click.argument("snaps", nargs=-1, type=str, required=True, metavar="<snap>...")
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
