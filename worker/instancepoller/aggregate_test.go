// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package instancepoller

import (
	"fmt"
	"sync"
	"time"

	gc "launchpad.net/gocheck"

	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/errors"
	"launchpad.net/juju-core/instance"
	jc "launchpad.net/juju-core/testing/checkers"
	"launchpad.net/juju-core/testing/testbase"
)

type aggregateSuite struct {
	testbase.LoggingSuite
}

var _ = gc.Suite(&aggregateSuite{})

type testInstance struct {
	instance.Instance
	addresses []instance.Address
	status    string
	err       error
}

var _ instance.Instance = (*testInstance)(nil)

func (t *testInstance) Addresses() ([]instance.Address, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.addresses, nil
}

func (t *testInstance) Status() string {
	return t.status
}

type testInstanceGetter struct {
	ids     []instance.Id
	results []instance.Instance
	err     error
	counter int
}

func (i *testInstanceGetter) Instances(ids []instance.Id) (result []instance.Instance, err error) {
	i.ids = ids
	err = i.err
	i.counter += 1
	for _, inst := range i.results {
		if inst == nil {
			result = append(result, nil)
		} else {
			result = append(result, inst)
		}
	}
	return
}

func newTestInstance(status string, addresses []string) *testInstance {
	thisInstance := testInstance{status: status}
	for _, address := range addresses {
		thisInstance.addresses = append(thisInstance.addresses, instance.NewAddress(address))
	}
	return &thisInstance
}

func (s *aggregateSuite) TestSingleRequest(c *gc.C) {
	testGetter := new(testInstanceGetter)
	instance1 := newTestInstance("foobar", []string{"127.0.0.1", "192.168.1.1"})
	testGetter.results = []instance.Instance{instance1}
	aggregator := newAggregator(testGetter)

	info, err := aggregator.instanceInfo("foo")
	c.Assert(err, gc.IsNil)
	c.Assert(info, gc.DeepEquals, instanceInfo{
		status:    "foobar",
		addresses: instance1.addresses,
	})
	c.Assert(testGetter.ids, gc.DeepEquals, []instance.Id{"foo"})
}

func (s *aggregateSuite) TestMultipleResponseHandling(c *gc.C) {
	s.PatchValue(&gatherTime, 30*time.Millisecond)
	testGetter := new(testInstanceGetter)

	instance1 := newTestInstance("foobar", []string{"127.0.0.1", "192.168.1.1"})
	testGetter.results = []instance.Instance{instance1}
	aggregator := newAggregator(testGetter)

	replyChan := make(chan instanceInfoReply)
	req := instanceInfoReq{
		reply:  replyChan,
		instId: instance.Id("foo"),
	}
	aggregator.reqc <- req
	reply := <-replyChan
	c.Assert(reply.err, gc.IsNil)

	instance2 := newTestInstance("not foobar", []string{"192.168.1.2"})
	instance3 := newTestInstance("ok-ish", []string{"192.168.1.3"})
	testGetter.results = []instance.Instance{instance2, instance3}

	var wg sync.WaitGroup
	checkInfo := func(id instance.Id, expectStatus string) {
		info, err := aggregator.instanceInfo(id)
		c.Check(err, gc.IsNil)
		c.Check(info.status, gc.Equals, expectStatus)
		wg.Done()
	}

	wg.Add(2)
	go checkInfo("foo2", "not foobar")
	go checkInfo("foo3", "ok-ish")
	wg.Wait()

	c.Assert(len(testGetter.ids), gc.DeepEquals, 2)
}

func (s *aggregateSuite) TestBatching(c *gc.C) {
	s.PatchValue(&gatherTime, 10*time.Millisecond)
	testGetter := new(testInstanceGetter)

	aggregator := newAggregator(testGetter)
	for i := 0; i < 100; i++ {
		testGetter.results = append(testGetter.results, newTestInstance("foobar", []string{"127.0.0.1", "192.168.1.1"}))
	}
	var wg sync.WaitGroup
	makeRequest := func() {
		_, err := aggregator.instanceInfo("foo")
		c.Check(err, gc.IsNil)
		wg.Done()
	}
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go makeRequest()
		time.Sleep(time.Millisecond)
	}
	wg.Wait()
	c.Assert(testGetter.counter, jc.LessThan, 15)
	c.Assert(testGetter.counter, jc.GreaterThan, 10)
}

func (s *aggregateSuite) TestError(c *gc.C) {
	testGetter := new(testInstanceGetter)
	ourError := fmt.Errorf("Some error")
	testGetter.err = ourError

	aggregator := newAggregator(testGetter)

	_, err := aggregator.instanceInfo("foo")
	c.Assert(err, gc.Equals, ourError)
}

func (s *aggregateSuite) TestPartialErrResponse(c *gc.C) {
	testGetter := new(testInstanceGetter)
	testGetter.err = environs.ErrPartialInstances
	testGetter.results = []instance.Instance{nil}

	aggregator := newAggregator(testGetter)
	_, err := aggregator.instanceInfo("foo")

	c.Assert(err, gc.DeepEquals, errors.NotFoundf("instance foo"))
}

func (s *aggregateSuite) TestAddressesError(c *gc.C) {
	testGetter := new(testInstanceGetter)
	instance1 := newTestInstance("foobar", []string{"127.0.0.1", "192.168.1.1"})
	ourError := fmt.Errorf("gotcha")
	instance1.err = ourError
	testGetter.results = []instance.Instance{instance1}

	aggregator := newAggregator(testGetter)
	_, err := aggregator.instanceInfo("foo")
	c.Assert(err, gc.Equals, ourError)
}

func (s *aggregateSuite) TestKillAndWait(c *gc.C) {
	testGetter := new(testInstanceGetter)
	aggregator := newAggregator(testGetter)
	aggregator.Kill()
	err := aggregator.Wait()
	c.Assert(err, gc.IsNil)
}
