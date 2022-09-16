import sys

from .fetch import Fetcher, fetch_op  # noqa: F401
from .simulation import auto_connections_op  # noqa: F401

if not sys.warnoptions:
    import warnings

    warnings.simplefilter("ignore")
