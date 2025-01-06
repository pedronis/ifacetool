// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2025 Canonical Ltd
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

func (am *assertsMock) mockSnapDecl(publisher string, extraHeaders map[string]interface{}) error {
	_, err := am.db.Find(asserts.AccountType, map[string]string{
		"account-id": publisher,
	})
	if errors.Is(err, &asserts.NotFoundError{}) {
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
	if err != nil {
		return err
	}
	headers["format"] = strconv.Itoa(fnum)

	snapDecl, err := am.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	if err != nil {
		return err
	}

	err = am.db.Add(snapDecl)
	noerror(err)

	return nil
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

func (s *oneshotSimulation) mockSnap(yamlText string) (*snap.Info, *asserts.SnapDeclaration, error) {
	sideInfo := &snap.SideInfo{
		Revision: snap.R(1),
	}
	snapInfo, err := mockDiskSnap(yamlText, sideInfo)
	if err != nil {
		return nil, nil, err
	}
	sideInfo.RealName = snapInfo.SnapName()

	var decl *asserts.SnapDeclaration
	a, err := s.db.FindMany(asserts.SnapDeclarationType, map[string]string{
		"snap-name": sideInfo.RealName,
	})
	if err == nil {
		decl = a[0].(*asserts.SnapDeclaration)
		snapInfo.SnapID = decl.SnapID()
		sideInfo.SnapID = decl.SnapID()
	} else if errors.Is(err, &asserts.NotFoundError{}) {
		err = nil
	}
	noerror(err)

	s.state.Lock()
	defer s.state.Unlock()

	// Put a side info into the state
	snapstate.Set(s.state, snapInfo.InstanceName(), &snapstate.SnapState{
		Active:      true,
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:     sideInfo.Revision,
		SnapType:    string(snapInfo.Type()),
		InstanceKey: snapInfo.InstanceKey,
	})
	return snapInfo, decl, nil
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

func checkInstall(modelAs *asserts.Model, info *snap.Info, decl *asserts.SnapDeclaration) installation {
	baseDecl := asserts.BuiltinBaseDeclaration()

	ic := policy.InstallCandidate{
		Snap:            info,
		SnapDeclaration: decl,

		BaseDeclaration: baseDecl,

		Model: modelAs,
		// XXX Store
	}

	err := ic.Check()
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	return installation{
		SnapName:      info.SnapName(),
		Error:         errStr,
		BadInterfaces: info.BadInterfaces,
	}
}

type autoConnectSimulation struct {
	Classic bool `json:"classic"`

	Brand string `json:"brand"`
	Model string `json:"model"`
	Store string `json:"store"`

	TargetSnap string   `json:"target-snap"`
	Snaps      []string `json:"snaps"`
}

type installation struct {
	SnapName      string            `json:"snap-name"`
	Error         string            `json:"error"`
	BadInterfaces map[string]string `json:"bad-interfaces,omitempty"`
}

type side struct {
	Interface string `json:"interface"`
	Name      string `json:"name"`
}

type connection struct {
	Interface string             `json:"interface"`
	PlugRef   interfaces.PlugRef `json:"plug"`
	SlotRef   interfaces.SlotRef `json:"slot"`
	// plug and/or slot are on the target snap
	OnTarget []string `json:"on-target"`
}

type candidate struct {
	Interface string             `json:"interface"`
	PlugRef   interfaces.PlugRef `json:"plug"`
	SlotRef   interfaces.SlotRef `json:"slot"`

	PlugStaticAttrs  map[string]interface{} `json:"plug-static-attrs"`
	PlugDynamicAttrs map[string]interface{} `json:"plug-dynamic-attrs"`
	SlotStaticAttrs  map[string]interface{} `json:"slot-static-attrs"`
	SlotDynamicAttrs map[string]interface{} `json:"slot-dynamic-attrs"`

	CheckError string `json:"check-error"`

	SlotsPerPlugAny bool `json:"slots-per-plug-any"`
}

type autoConnectSimulationResult struct {
	targetSnap string

	Installing []installation `json:"installing"`

	Plugs []side `json:"plugs"`
	Slots []side `json:"slots"`

	Connections []connection `json:"connections"`

	SlotCandidates map[string][]candidate `json:"slot-candidates"`
	PlugCandidates map[string][]candidate `json:"plug-candidates"`
}

func (r *autoConnectSimulationResult) debugAutoConnectCheck(cc *policy.ConnectCandidate, arity interfaces.SideArity, checkErr error) {
	var cand candidate
	cand.Interface = cc.Plug.Interface()
	cand.PlugRef = *cc.Plug.Ref()
	cand.SlotRef = *cc.Slot.Ref()
	cand.PlugStaticAttrs = cc.Plug.StaticAttrs()
	cand.PlugDynamicAttrs = cc.Plug.DynamicAttrs()
	cand.SlotStaticAttrs = cc.Slot.StaticAttrs()
	cand.SlotDynamicAttrs = cc.Slot.DynamicAttrs()
	if checkErr != nil {
		cand.CheckError = checkErr.Error()
	} else {
		cand.SlotsPerPlugAny = arity.SlotsPerPlugAny()
	}
	if cand.PlugRef.Snap == r.targetSnap {
		r.SlotCandidates[cand.PlugRef.Name] = append(r.SlotCandidates[cand.PlugRef.Name], cand)
	}
	if cand.SlotRef.Snap == r.targetSnap {
		r.PlugCandidates[cand.SlotRef.Name] = append(r.PlugCandidates[cand.SlotRef.Name], cand)
	}
}

func (s *oneshotSimulation) simulateAutoConnect(params *autoConnectSimulation) error {
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
		if err := s.mockSnapDecl(ref.PublisherID, d); err != nil {
			return fmt.Errorf("processing snap %s rules: %v", name, err)
		}
	}

	targetSnap := params.TargetSnap
	var targetInfo *snap.Info

	var res autoConnectSimulationResult
	// wire-up things for candidate collection
	res.targetSnap = targetSnap
	ifacestate.DebugAutoConnectCheck = res.debugAutoConnectCheck
	res.SlotCandidates = make(map[string][]candidate)
	res.PlugCandidates = make(map[string][]candidate)
	// Add snap metadata, and populate repo
	for name := range seen {
		snapYamlFn := filepath.Join(name, "snap.yaml")
		b, err := ioutil.ReadFile(snapYamlFn)
		noerror(err)
		snapInfo, snapDecl, err := s.mockSnap(string(b))
		if err != nil {
			return fmt.Errorf("processing snap %s: %v", name, err)
		}

		inst := checkInstall(modelAs, snapInfo, snapDecl)
		snapAppSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
		if err != nil {
			return fmt.Errorf("processing snap %s: %v", name, err)
		}

		err = mgr.Repository().AddAppSet(snapAppSet)
		if err != nil {
			return fmt.Errorf("processing snap %s: %v", snapInfo.SnapName(), err)
		}

		res.Installing = append(res.Installing, inst)

		if name != targetSnap {
			continue
		}
		targetInfo = snapInfo

		for plugName, plug := range targetInfo.Plugs {
			res.Plugs = append(res.Plugs, side{
				Interface: plug.Interface,
				Name:      plugName,
			})
		}

		for slotName, slot := range targetInfo.Slots {
			res.Slots = append(res.Slots, side{
				Interface: slot.Interface,
				Name:      slotName,
			})
		}
	}

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(&snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: targetSnap,
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

	for _, t := range change.Tasks() {
		if t.Kind() == "connect" {
			var plugRef interfaces.PlugRef
			var slotRef interfaces.SlotRef
			t.Get("plug", &plugRef)
			t.Get("slot", &slotRef)
			var iface string
			var onTarget []string
			if slotRef.Snap == targetSnap {
				onTarget = append(onTarget, "slot")
				iface = targetInfo.Slots[slotRef.Name].Interface
			}
			if plugRef.Snap == targetSnap {
				onTarget = append(onTarget, "plug")
				iface = targetInfo.Plugs[plugRef.Name].Interface
			}
			res.Connections = append(res.Connections, connection{
				Interface: iface,
				PlugRef:   plugRef,
				SlotRef:   slotRef,
				OnTarget:  onTarget,
			})
		}
	}

	b, err := json.Marshal(&res)
	noerror(err)
	fmt.Println(string(b))
	return nil
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

func mockDiskSnap(yamlText string, sideInfo *snap.SideInfo) (*snap.Info, error) {
	// Parse the yaml (we need the Name).
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	if err != nil {
		return nil, err
	}

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

	return snapInfo, nil
}

// Operations

func autoConnections(param *json.RawMessage) error {
	var params autoConnectSimulation
	if err := json.Unmarshal([]byte(*param), &params); err != nil {
		return err
	}

	sim := oneshotSimulation{}
	sim.setup(params.Classic)
	err := sim.simulateAutoConnect(&params)
	if err != nil {
		var errRes struct {
			Error string `json:"error"`
		}
		errRes.Error = err.Error()
		b, err := json.Marshal(&errRes)
		noerror(err)
		fmt.Println(string(b))
		return nil
	}
	sim.finish()

	return nil
}
