package main

import (
	"testing"

	"dabbertorres.dev/dbman"
	"github.com/neovim/go-client/nvim"
	"github.com/neovim/go-client/nvim/plugin"
)

func initTestEnv(t *testing.T, register func(*plugin.Plugin)) (n *nvim.Nvim, p *plugin.Plugin) {
	var err error
	n, err = nvim.NewChildProcess(
		nvim.ChildProcessArgs(
			"--embed",
			"--headless",
			"-n",
		),
		nvim.ChildProcessServe(true),
	)
	if err != nil {
		t.Fatal(err)
	}

	p = plugin.New(n)
	register(p)

	if err := p.RegisterForTests(); err != nil {
		t.Fatal(err)
	}

	return n, p
}

func Test_listConnections(t *testing.T) {
	cfg := dbman.Config{
		Connections: map[string]dbman.Connection{
			"local": {
				Host:              "localhost",
				Port:              5432,
				Database:          "postgres",
				Username:          "postgres",
				Password:          "postgres",
				Driver:            "postgres",
				Tunnel:            "",
				ConnectTimeoutSec: 30,
				MaxOpenConns:      4,
			},
		},
	}
	state := &pluginState{
		db:           dbman.New(&cfg),
		displayBuf:   -1,
		displayWin:   -1,
		displayCache: make(map[string][]schemaState),
	}
	defer state.db.Close()

	n, _ := initTestEnv(t, func(p *plugin.Plugin) {
		p.HandleCommand(listConnections(state))
	})
	defer n.Close()

	output, err := n.CommandOutput("DBConnections")
	if err != nil {
		t.Error(err)
	}

	if output != "local" {
		t.Errorf("expected output to be 'local', but was: '%s'", output)
	}
}
