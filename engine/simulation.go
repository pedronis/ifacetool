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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snap"
)

// XXX
func noerror(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "simulation error: %v\n", err)
		os.Exit(1)
	}
}

type assertsMock struct {
	db           *asserts.Database
	storeSigning *assertstest.StoreStack
	st           *state.State
}

func (am *assertsMock) setupAsserts(st *state.State) {
	am.st = st
	am.storeSigning = assertstest.NewStoreStack("canonical", nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   am.storeSigning.Trusted,
	})
	noerror(err)
	am.db = db
	err = db.Add(am.storeSigning.StoreAccountKey(""))
	noerror(err)

	st.Lock()
	assertstate.ReplaceDB(st, am.db)
	st.Unlock()
}

func (am *assertsMock) mockModel(extraHeaders map[string]interface{}) *asserts.Model {
	modHeaders := map[string]interface{}{
		"type":         "model",
		"series":       "16",
		"gadget":       "gadget",
		"kernel":       "kernel",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	model := assertstest.FakeAssertion(modHeaders, extraHeaders).(*asserts.Model)
	snapstatetest.MockDeviceModel(model)
	return model
}

func (am *assertsMock) mockSnapDecl(publisher string, extraHeaders map[string]interface{}) {
	_, err := am.db.Find(asserts.AccountType, map[string]string{
		"account-id": publisher,
	})
	if asserts.IsNotFound(err) {
		acct := assertstest.NewAccount(am.storeSigning, publisher, map[string]interface{}{
			"account-id": publisher,
		}, "")
		err = am.db.Add(acct)
	}
	noerror(err)

	headers := map[string]interface{}{
		"series":    "16",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	fnum, err := asserts.SuggestFormat(asserts.SnapDeclarationType, headers, nil)
	noerror(err)
	headers["format"] = strconv.Itoa(fnum)

	snapDecl, err := am.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	noerror(err)

	err = am.db.Add(snapDecl)
	noerror(err)
}

func (am *assertsMock) mockStore(st *state.State, storeID string, extraHeaders map[string]interface{}) {
	headers := map[string]interface{}{
		"store":       storeID,
		"operator-id": am.storeSigning.AuthorityID,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	storeAs, err := am.storeSigning.Sign(asserts.StoreType, headers, nil, "")
	noerror(err)
	st.Lock()
	defer st.Unlock()
	err = assertstate.Add(st, storeAs)
	noerror(err)
}

// oneshotSimulation simulate one interface manager behavior at a time,
// see simulate* methods
// it does not cleanup after itself!
type oneshotSimulation struct {
	assertsMock
	o          *overlord.Overlord
	state      *state.State
	se         *overlord.StateEngine
	mgr        *ifacestate.InterfaceManager
	hookMgr    *hookstate.HookManager
	secBackend *ifacetest.TestSecurityBackend
	log        *bytes.Buffer
}

func (s *oneshotSimulation) setup(classic bool) {
	release.MockOnClassic(classic)

	tmpdir, err := ioutil.TempDir("", "ifacesimu")
	noerror(err)
	dirs.SetRootDir(tmpdir)
	noerror(os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755))

	// needed for system key generation
	osutil.MockMountInfo("")

	s.o = overlord.Mock()
	s.state = s.o.State()
	s.se = s.o.StateEngine()

	s.setupAsserts(s.state)

	s.state.Lock()
	defer s.state.Unlock()

	s.secBackend = &ifacetest.TestSecurityBackend{}
	ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{s.secBackend})
	apparmor_sandbox.MockFeatures([]string{
		"caps",
		"dbus",
		"domain",
		"file",
		"mount",
		"namespaces",
		"network",
		"ptrace",
		"signal",
	}, nil, []string{
		"unsafe",
		"qipcrtr-socket",
		"cap-bpf",
		"cap-audit-read",
		"mqueue",
	}, nil)

	buf, _ := logger.MockLogger()
	s.log = buf

	ifacestate.MockConnectRetryTimeout(0)
	seccomp_compiler.MockCompilerVersionInfo("abcdef 1.2.3 1234abcd -")
}

func (s *oneshotSimulation) finish() {
	s.se.Stop()
}

func addForeignTaskHandlers(runner *state.TaskRunner) {
	// Add handler to test full aborting of changes
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	runner.AddHandler("error-trigger", erroringHandler, nil)
}

func (s *oneshotSimulation) manager() *ifacestate.InterfaceManager {
	if s.mgr != nil {
		noerror(fmt.Errorf("internal error: interface manager already initialized"))
	}
	s.hookMgr = s.hookManager()
	mgr, err := ifacestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), nil, nil)
	noerror(err)
	addForeignTaskHandlers(s.o.TaskRunner())
	mgr.DisableUDevMonitor()
	s.mgr = mgr
	s.o.AddManager(mgr)

	s.o.AddManager(s.o.TaskRunner())

	err = s.o.StartUp()
	noerror(err)

	// ensure the re-generation of security profiles did not
	// confuse the tests
	s.secBackend.SetupCalls = nil
	return s.mgr
}

func (s *oneshotSimulation) hookManager() *hookstate.HookManager {
	mgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
	noerror(err)
	s.o.AddManager(mgr)
	return mgr
}

func (s *oneshotSimulation) mockSnap(yamlText string) (*snap.Info, *asserts.SnapDeclaration) {
	sideInfo := &snap.SideInfo{
		Revision: snap.R(1),
	}
	snapInfo := mockDiskSnap(yamlText, sideInfo)
	sideInfo.RealName = snapInfo.SnapName()

	var decl *asserts.SnapDeclaration
	a, err := s.db.FindMany(asserts.SnapDeclarationType, map[string]string{
		"snap-name": sideInfo.RealName,
	})
	if err == nil {
		decl = a[0].(*asserts.SnapDeclaration)
		snapInfo.SnapID = decl.SnapID()
		sideInfo.SnapID = decl.SnapID()
	} else if asserts.IsNotFound(err) {
		err = nil
	}
	noerror(err)

	s.state.Lock()
	defer s.state.Unlock()

	// Put a side info into the state
	snapstate.Set(s.state, snapInfo.InstanceName(), &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{sideInfo},
		Current:     sideInfo.Revision,
		SnapType:    string(snapInfo.Type()),
		InstanceKey: snapInfo.InstanceKey,
	})
	return snapInfo, decl
}

func (s *oneshotSimulation) addSetupSnapSecurityChange(snapsup *snapstate.SnapSetup) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	change := s.state.NewChange("test", "")
	task1 := s.state.NewTask("auto-connect", "")
	task1.Set("snap-setup", snapsup)
	change.AddTask(task1)
	return change
}

var snapdSnapYaml = `
name: snapd
version: 1
type: snapd
`

func checkInstall(modelAs *asserts.Model, info *snap.Info, decl *asserts.SnapDeclaration) {
	baseDecl := asserts.BuiltinBaseDeclaration()

	ic := policy.InstallCandidate{
		Snap:            info,
		SnapDeclaration: decl,

		BaseDeclaration: baseDecl,

		Model: modelAs,
		// XXX Store
	}

	err := ic.Check()
	fmt.Printf("installing %s: %v\n", info.SnapName(), err)
	if len(info.BadInterfaces) != 0 {
		fmt.Printf("bad-interfaces: %v\n", info.BadInterfaces)
	}
}

type autoConnectSimulation struct {
	Brand string `json:"brand"`
	Model string `json:"model"`
	Store string `json:"store"`

	TargetSnap string   `json:"target-snap"`
	Snaps      []string `json:"snaps"`
}

func (s *oneshotSimulation) simulateAutoConnect(params *autoConnectSimulation) {
	modelHdrs := map[string]interface{}{
		"authority-id": params.Brand,
		"brand-id":     params.Brand,
		"model":        params.Model,
	}
	if params.Store != "" {
		modelHdrs["store"] = params.Store
	}
	modelAs := s.mockModel(modelHdrs)
	if params.Store != "" {
		s.mockStore(s.state, params.Store, nil)
	}

	// Add a snapd snap.
	s.mockSnap(snapdSnapYaml)

	// Initialize the manager. This registers the system snap.
	mgr := s.manager()

	snaps := params.Snaps
	snaps = append(snaps, params.TargetSnap)
	seen := make(map[string]bool, len(snaps))

	// Add declarations
	for _, name := range snaps {
		if seen[name] {
			continue
		}
		seen[name] = true
		ref, err := readRef(name)
		noerror(err)
		d := map[string]interface{}{
			"snap-name":    ref.SnapName,
			"snap-id":      ref.SnapID,
			"publisher-id": ref.PublisherID,
		}
		if plugs, err := loadJSON(filepath.Join(name, "plugs.json")); !os.IsNotExist(err) {
			noerror(err)
			d["plugs"] = plugs
		}
		if slots, err := loadJSON(filepath.Join(name, "slots.json")); !os.IsNotExist(err) {
			noerror(err)
			d["slots"] = slots
		}
		s.mockSnapDecl(ref.PublisherID, d)
	}

	// Add snap metadata, and populate repo
	for name := range seen {
		snapYamlFn := filepath.Join(name, "snap.yaml")
		b, err := ioutil.ReadFile(snapYamlFn)
		noerror(err)
		snapInfo, snapDecl := s.mockSnap(string(b))

		checkInstall(modelAs, snapInfo, snapDecl)

		err = mgr.Repository().AddSnap(snapInfo)
		noerror(err)
	}

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(&snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: params.TargetSnap,
			Revision: snap.R(1),
		},
	})
	err := s.se.Ensure()
	noerror(err)
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	err = change.Err()
	noerror(err)

	var conns []string
	for _, t := range change.Tasks() {
		if t.Kind() == "connect" {
			var plugRef interfaces.PlugRef
			var slotRef interfaces.SlotRef
			t.Get("plug", &plugRef)
			t.Get("slot", &slotRef)
			conns = append(conns, fmt.Sprintf("%v > %v", slotRef, plugRef))
		}
	}
	sort.Strings(conns)
	for _, conn := range conns {
		fmt.Println(conn)
	}
}

func loadJSON(fn string) (res map[string]interface{}, err error) {
	b, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func mockDiskSnap(yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	// Parse the yaml (we need the Name).
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	noerror(err)

	// Set SideInfo so that we can use MountDir below
	snapInfo.SideInfo = *sideInfo

	// Put the YAML on disk, in the right spot.
	metaDir := filepath.Join(snapInfo.MountDir(), "meta")
	err = os.MkdirAll(metaDir, 0755)
	noerror(err)
	err = ioutil.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(yamlText), 0644)
	noerror(err)

	// Write the .snap to disk
	err = os.MkdirAll(filepath.Dir(snapInfo.MountFile()), 0755)
	noerror(err)
	snapContents := fmt.Sprintf("%s-%s-%s", sideInfo.RealName, sideInfo.SnapID, sideInfo.Revision)
	err = ioutil.WriteFile(snapInfo.MountFile(), []byte(snapContents), 0644)
	noerror(err)
	snapInfo.Size = int64(len(snapContents))

	return snapInfo
}

// Operations

func autoConnections(param *json.RawMessage) error {
	var params autoConnectSimulation
	if err := json.Unmarshal([]byte(*param), &params); err != nil {
		return err
	}

	sim := oneshotSimulation{}
	// assume Ubuntu Core
	sim.setup(false)
	sim.simulateAutoConnect(&params)
	sim.finish()

	return nil
}
