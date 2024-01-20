VENV := ./venv/bin
TOOLENV := ./toolenv/bin
VERSION := $(shell cat VERSION)
SNAPD_VERSION := $(shell cat SNAPD_VERSION)

venv:
	python3 -m venv venv
	$(VENV)/pip3 install -r requirements.txt

toolenv:
	python3 -m venv toolenv
	$(TOOLENV)/pip3 install flake8 black

clean-envs:
	rm -rf venv toolenv

stylelint:
	$(TOOLENV)/black ifacetool.py ops
	$(TOOLENV)/flake8 --ignore E203 --max-line-length=99 ifacetool.py ops

get-snapd:
	go get github.com/snapcore/snapd@$(SNAPD_VERSION)
	go mod tidy

local-replace-on:
	sed -i -e 's#^// replace#replace#' go.mod

local-replace-off:
	sed -i -e 's#^replace#// replace#' go.mod

# a built ifacetool-engine is needed to run ifacetool.py from source,
# the snap builds one as a part
ifacetool-engine:
	go build -o ifacetool-engine ./engine

ifacetool_$(VERSION)-$(SNAPD_VERSION)_amd64.snap:
	snapcraft snap

clean:
	rm -f ifacetool-engine
	rm -f *.snap
	rm -rf temp

.PHONY: stylelint get-snapd local-replace-off local-replace-on clean clean-envs
