// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs_test

import (
	"time"

	gc "launchpad.net/gocheck"
	"launchpad.net/goyaml"

	"launchpad.net/juju-core/agent/tools"
	"launchpad.net/juju-core/cert"
	"launchpad.net/juju-core/constraints"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/environs/cloudinit"
	"launchpad.net/juju-core/environs/config"
	"launchpad.net/juju-core/juju/osenv"
	"launchpad.net/juju-core/state"
	"launchpad.net/juju-core/state/api"
	"launchpad.net/juju-core/testing"
	"launchpad.net/juju-core/utils"
	"launchpad.net/juju-core/version"
)

type CloudInitSuite struct{}

var _ = gc.Suite(&CloudInitSuite{})

func (s *CloudInitSuite) TestFinishInstanceConfig(c *gc.C) {
	cfg, err := config.New(map[string]interface{}{
		"name":            "barbara",
		"type":            "dummy",
		"authorized-keys": "we-are-the-keys",
		"ca-cert":         testing.CACert,
		"ca-private-key":  "",
	})
	c.Assert(err, gc.IsNil)
	mcfg := &cloudinit.MachineConfig{
		StateInfo: &state.Info{Tag: "not touched"},
		APIInfo:   &api.Info{Tag: "not touched"},
	}
	err = environs.FinishMachineConfig(mcfg, cfg, constraints.Value{})
	c.Assert(err, gc.IsNil)
	c.Assert(mcfg, gc.DeepEquals, &cloudinit.MachineConfig{
		AuthorizedKeys:     "we-are-the-keys",
		MachineEnvironment: map[string]string{osenv.JujuProviderType: "dummy"},
		StateInfo:          &state.Info{Tag: "not touched"},
		APIInfo:            &api.Info{Tag: "not touched"},
	})
}

func (s *CloudInitSuite) TestFinishBootstrapConfig(c *gc.C) {
	cfg, err := config.New(map[string]interface{}{
		"name":            "barbara",
		"type":            "dummy",
		"admin-secret":    "lisboan-pork",
		"authorized-keys": "we-are-the-keys",
		"agent-version":   "1.2.3",
		"ca-cert":         testing.CACert,
		"ca-private-key":  testing.CAKey,
		"state-server":    false,
		"secret":          "british-horse",
	})
	c.Assert(err, gc.IsNil)
	oldAttrs := cfg.AllAttrs()
	mcfg := &cloudinit.MachineConfig{
		StateServer: true,
	}
	cons := constraints.MustParse("mem=1T cpu-power=999999999")
	err = environs.FinishMachineConfig(mcfg, cfg, cons)
	c.Check(err, gc.IsNil)
	c.Check(mcfg.AuthorizedKeys, gc.Equals, "we-are-the-keys")
	password := utils.PasswordHash("lisboan-pork")
	c.Check(mcfg.APIInfo, gc.DeepEquals, &api.Info{
		Password: password, CACert: []byte(testing.CACert),
	})
	c.Check(mcfg.StateInfo, gc.DeepEquals, &state.Info{
		Password: password, CACert: []byte(testing.CACert),
	})
	c.Check(mcfg.StatePort, gc.Equals, cfg.StatePort())
	c.Check(mcfg.APIPort, gc.Equals, cfg.APIPort())
	c.Check(mcfg.Constraints, gc.DeepEquals, cons)

	oldAttrs["ca-private-key"] = ""
	oldAttrs["admin-secret"] = ""
	delete(oldAttrs, "secret")
	c.Check(mcfg.Config.AllAttrs(), gc.DeepEquals, oldAttrs)
	srvCertPEM := mcfg.StateServerCert
	srvKeyPEM := mcfg.StateServerKey
	_, _, err = cert.ParseCertAndKey(srvCertPEM, srvKeyPEM)
	c.Check(err, gc.IsNil)

	err = cert.Verify(srvCertPEM, []byte(testing.CACert), time.Now())
	c.Assert(err, gc.IsNil)
	err = cert.Verify(srvCertPEM, []byte(testing.CACert), time.Now().AddDate(9, 0, 0))
	c.Assert(err, gc.IsNil)
	err = cert.Verify(srvCertPEM, []byte(testing.CACert), time.Now().AddDate(10, 0, 1))
	c.Assert(err, gc.NotNil)
}

func (*CloudInitSuite) TestUserData(c *gc.C) {
	testJujuHome := c.MkDir()
	defer config.SetJujuHome(config.SetJujuHome(testJujuHome))
	tools := &tools.Tools{
		URL:     "http://foo.com/tools/juju1.2.3-linux-amd64.tgz",
		Version: version.MustParseBinary("1.2.3-linux-amd64"),
	}
	envConfig, err := config.New(map[string]interface{}{
		"type":            "maas",
		"name":            "foo",
		"default-series":  "series",
		"authorized-keys": "keys",
		"ca-cert":         testing.CACert,
	})
	c.Assert(err, gc.IsNil)

	cfg := &cloudinit.MachineConfig{
		MachineId:       "10",
		MachineNonce:    "5432",
		Tools:           tools,
		StateServerCert: []byte(testing.ServerCert),
		StateServerKey:  []byte(testing.ServerKey),
		StateInfo: &state.Info{
			Password: "pw1",
			CACert:   []byte("CA CERT\n" + testing.CACert),
		},
		APIInfo: &api.Info{
			Password: "pw2",
			CACert:   []byte("CA CERT\n" + testing.CACert),
		},
		DataDir:            environs.DataDir,
		Config:             envConfig,
		StatePort:          envConfig.StatePort(),
		APIPort:            envConfig.APIPort(),
		StateServer:        true,
		MachineEnvironment: map[string]string{osenv.JujuProviderType: "dummy"},
	}
	script1 := "script1"
	script2 := "script2"
	scripts := []string{script1, script2}
	result, err := environs.ComposeUserData(cfg, scripts...)
	c.Assert(err, gc.IsNil)

	unzipped, err := utils.Gunzip(result)
	c.Assert(err, gc.IsNil)

	config := make(map[interface{}]interface{})
	err = goyaml.Unmarshal(unzipped, &config)
	c.Assert(err, gc.IsNil)

	// Just check that the cloudinit config looks good.
	c.Check(config["apt_upgrade"], gc.Equals, true)
	// The scripts given to userData where added as the first
	// commands to be run.
	runCmd := config["runcmd"].([]interface{})
	c.Check(runCmd[0], gc.Equals, script1)
	c.Check(runCmd[1], gc.Equals, script2)
}
