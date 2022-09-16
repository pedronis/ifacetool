import click
from ops import (
    Fetcher,
    auto_connections_op,
    fetch_op,
)


@click.group()
def cli():
    pass


@cli.command()
@click.option("--decl-only", default=False, is_flag=True)
@click.argument("snaps", nargs=-1, type=str, required=True)
def fetch(snaps, decl_only):
    f = Fetcher()
    fetch_op(snaps, decl_only=decl_only, f=f)


@cli.command()
@click.option("--model", type=str, default="brand/model")
@click.option("--store", type=str, default=None)
@click.argument("target-snap", type=str, required=True)
@click.argument("context-snaps", type=str, nargs=-1)
def auto_connections(target_snap, context_snaps, model, store):
    f = Fetcher()
    auto_connections_op(target_snap, context_snaps, model=model, store=store, f=f)


if __name__ == "__main__":
    cli()
