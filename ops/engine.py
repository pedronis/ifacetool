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
            capture_output=True,
        ).stdout
    except subprocess.CalledProcessError as pe:
        raise Exception("{}, err: {}".format(pe, pe.stderr.decode("utf8")))
