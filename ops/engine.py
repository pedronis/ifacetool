from . import __path__

import json
import os
import subprocess


def engine(op, **params):
    engpgm = os.path.join(__path__[0], "..", "ifacetool-engine")
    if not os.path.isfile(engpgm):
        engpgm = os.path.basename(engpgm)
    param = json.dumps(params)
    try:
        return subprocess.run(
            [engpgm, op, param],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        ).stdout
    except subprocess.CalledProcessError as pe:
        raise Exception("{}, err: {}".format(pe, pe.stderr.decode("utf8")))
