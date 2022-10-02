// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/store/tooling"
)

type snapRef struct {
	SnapName    string `json:"snap-name"`
	SnapID      string `json:"snap-id"`
	PublisherID string `json:"publisher-id"`
}

func readRef(name string) (*snapRef, error) {
	var ref snapRef
	b, err := ioutil.ReadFile(filepath.Join(name, ".snap.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		return nil, fmt.Errorf("cannot parse %q .snap.json: %v", name, err)
	}
	return &ref, nil
}

func writeJSON(name, what string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal %q for %q: %v", what, name, err)
	}
	buf := bytes.NewBuffer(b)
	buf.WriteString("\n")
	return ioutil.WriteFile(filepath.Join(name, what), buf.Bytes(), 0644)
}

func fetchDecls(param *json.RawMessage) error {
	var params struct {
		Snaps []string `json:"snaps"`
	}

	if err := json.Unmarshal([]byte(*param), &params); err != nil {
		return err
	}

	tsto, err := tooling.NewToolingStore()
	if err != nil {
		return err
	}

	for _, name := range params.Snaps {
		ref, err := readRef(name)
		if err != nil {
			return err
		}
		a, err := tsto.Find(asserts.SnapDeclarationType, map[string]string{
			"series":  "16",
			"snap-id": ref.SnapID,
		})
		if err != nil {
			return err
		}
		decl := a.(*asserts.SnapDeclaration)
		hdrs := decl.Headers()
		plugs, plugsOK := hdrs["plugs"]
		slots, slotsOK := hdrs["slots"]
		if plugsOK {
			if err := writeJSON(name, "plugs.json", plugs); err != nil {
				return err
			}
		}
		if slotsOK {
			if err := writeJSON(name, "slots.json", slots); err != nil {
				return err
			}
		}
	}
	return nil
}
