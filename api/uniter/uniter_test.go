// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package uniter_test

import (
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/uniter"
	"github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
)

// NOTE: This suite is intended for embedding into other suites,
// so common code can be reused. Do not add test cases to it,
// otherwise they'll be run by each other suite that embeds it.
type uniterSuite struct {
	testing.JujuConnSuite

	st                api.Connection
	controllerMachine *state.Machine
	wordpressMachine  *state.Machine
	wordpressService  *state.Application
	wordpressCharm    *state.Charm
	wordpressUnit     *state.Unit

	uniter *uniter.State
}

var _ = gc.Suite(&uniterSuite{})

func (s *uniterSuite) SetUpTest(c *gc.C) {
	s.setUpTest(c, true)
}

func (s *uniterSuite) setUpTest(c *gc.C, addController bool) {
	s.JujuConnSuite.SetUpTest(c)

	if addController {
		s.controllerMachine = testing.AddControllerMachine(c, s.State)
	}

	// Bind "db" relation of wordpress to space "internal",
	// and the "admin-api" extra-binding to space "public".
	bindings := map[string]string{
		"db":        "internal",
		"admin-api": "public",
	}
	_, err := s.State.AddSpace("internal", "", nil, false)
	c.Assert(err, jc.ErrorIsNil)
	_, err = s.State.AddSpace("public", "", nil, true)
	c.Assert(err, jc.ErrorIsNil)

	// Create a machine, a service and add a unit so we can log in as
	// its agent.
	s.wordpressMachine, s.wordpressService, s.wordpressCharm, s.wordpressUnit = s.addMachineBoundServiceCharmAndUnit(c, "wordpress", bindings)
	password, err := utils.RandomPassword()
	c.Assert(err, jc.ErrorIsNil)
	err = s.wordpressUnit.SetPassword(password)
	c.Assert(err, jc.ErrorIsNil)
	s.st = s.OpenAPIAs(c, s.wordpressUnit.Tag(), password)

	// Create the uniter API facade.
	s.uniter, err = s.st.Uniter()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(s.uniter, gc.NotNil)
}

func (s *uniterSuite) addMachineBoundServiceCharmAndUnit(c *gc.C, serviceName string, bindings map[string]string) (*state.Machine, *state.Application, *state.Charm, *state.Unit) {
	machine, err := s.State.AddMachine("quantal", state.JobHostUnits)
	c.Assert(err, jc.ErrorIsNil)
	charm := s.AddTestingCharm(c, serviceName)

	service, err := s.State.AddApplication(state.AddApplicationArgs{
		Name:             serviceName,
		Charm:            charm,
		EndpointBindings: bindings,
	})
	c.Assert(err, jc.ErrorIsNil)

	unit, err := service.AddUnit()
	c.Assert(err, jc.ErrorIsNil)

	err = unit.AssignToMachine(machine)
	c.Assert(err, jc.ErrorIsNil)

	return machine, service, charm, unit
}

func (s *uniterSuite) addMachineServiceCharmAndUnit(c *gc.C, serviceName string) (*state.Machine, *state.Application, *state.Charm, *state.Unit) {
	return s.addMachineBoundServiceCharmAndUnit(c, serviceName, nil)
}

func (s *uniterSuite) addRelation(c *gc.C, first, second string) *state.Relation {
	eps, err := s.State.InferEndpoints(first, second)
	c.Assert(err, jc.ErrorIsNil)
	rel, err := s.State.AddRelation(eps...)
	c.Assert(err, jc.ErrorIsNil)
	return rel
}

func (s *uniterSuite) addRelatedService(c *gc.C, firstSvc, relatedSvc string, unit *state.Unit) (*state.Relation, *state.Application, *state.Unit) {
	relatedService := s.AddTestingService(c, relatedSvc, s.AddTestingCharm(c, relatedSvc))
	rel := s.addRelation(c, firstSvc, relatedSvc)
	relUnit, err := rel.Unit(unit)
	c.Assert(err, jc.ErrorIsNil)
	err = relUnit.EnterScope(nil)
	c.Assert(err, jc.ErrorIsNil)
	relatedUnit, err := s.State.Unit(relatedSvc + "/0")
	c.Assert(err, jc.ErrorIsNil)
	return rel, relatedService, relatedUnit
}

func (s *uniterSuite) assertInScope(c *gc.C, relUnit *state.RelationUnit, inScope bool) {
	ok, err := relUnit.InScope()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ok, gc.Equals, inScope)
}

func (s *uniterSuite) patchNewState(
	c *gc.C,
	patchFunc func(_ base.APICaller, _ names.UnitTag) *uniter.State,
) {
	s.PatchValue(&uniter.NewState, patchFunc)
	var err error
	s.uniter, err = s.st.Uniter()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(s.uniter, gc.NotNil)
}

func (s *uniterSuite) TestSLALevel(c *gc.C) {
	err := s.State.SetSLA("essential", "bob", []byte("creds"))
	c.Assert(err, jc.ErrorIsNil)

	level, err := s.uniter.SLALevel()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(level, gc.Equals, "essential")
}
