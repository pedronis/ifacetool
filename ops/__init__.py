import sys

from .fetch import Fetcher, fetch_op, snap_at_rev  # noqa: F401
from .simulation import auto_connections_op  # noqa: F401

if not sys.warnoptions:
    import warnings

    warnings.simplefilter("ignore")
