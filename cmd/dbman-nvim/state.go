package main

import (
	"fmt"
	"log"
	"strings"
	"text/tabwriter"

	"dabbertorres.dev/dbman"
	"github.com/neovim/go-client/nvim"
	"golang.org/x/crypto/ssh"
)

type dbManager interface {
	CurrentName() string
	ListConnections() (names []string, active []bool)
	SwitchConnection(connName string, prompter ssh.KeyboardInteractiveChallenge) error
	ListTables(schema string) ([]string, error)
	ListSchemas() ([]string, error)
	DescribeTable(name string) (*dbman.TableSchema, error)
	Query(script string) (*dbman.QueryResult, error)
}

type pluginState struct {
	db           dbManager
	displayCache map[string][]schemaState
	displayBuf   nvim.Buffer
	displayWin   nvim.Window
	outputBuf    nvim.Buffer
	outputWin    nvim.Window
}

type schemaState struct {
	Name   string
	Tables []dbman.TableSchema
}

func (s *pluginState) displaySchemas(api *nvim.Nvim, refreshCache bool) error {
	var (
		validBuf bool
		validWin bool

		currWin nvim.Window
		currBuf nvim.Buffer
	)
	batch := api.NewBatch()

	if s.displayBuf > 0 {
		batch.IsBufferLoaded(s.displayBuf, &validBuf)
	}
	if s.displayWin > 0 {
		batch.IsWindowValid(s.displayWin, &validWin)
	}

	batch.CurrentWindow(&currWin)
	batch.CurrentBuffer(&currBuf)
	if err := batch.Execute(); err != nil {
		return err
	}

	// always reset to user's current window/buffer
	defer func() {
		batch := api.NewBatch()
		batch.SetCurrentWindow(currWin)
		batch.SetCurrentBuffer(currBuf)
		if err := batch.Execute(); err != nil {
			log.Print("failed to reset focus to user's window and buffer: " + err.Error())
		}
	}()

	// TODO
	//if !validBuf {
	//	var err error
	//	s.displayBuf, err = api.CreateBuffer(true, true)
	//	if err != nil {
	//		return err
	//	}
	//	updateDisplays = true
	//}

	if !validWin {
		var err error
		s.displayBuf, s.displayWin, err = openSplitWindow(api, true, 0)
		if err != nil {
			return err
		}

		batch := api.NewBatch()
		batch.SetWindowOption(s.displayWin, "foldenable", true)
		batch.SetWindowOption(s.displayWin, "foldmethod", "indent")
		batch.SetWindowOption(s.displayWin, "foldlevel", 1)
		batch.SetWindowOption(s.displayWin, "foldminlines", 0)
		batch.SetBufferName(s.displayBuf, s.db.CurrentName())
		if err := batch.Execute(); err != nil {
			log.Print("failed to set window fold options and buffer name:", err)
		}
	}

	if refreshCache {
		api.WriteOut("refreshing cache...\n")
		if err := s.refreshCache(); err != nil {
			api.WritelnErr("failed: " + err.Error())
			return err
		}
		api.WriteOut("done\n")
	}

	api.WriteOut("schemas:\n")
	schemas := s.displayCache[s.db.CurrentName()]
	for _, schema := range schemas {
		api.WriteOut("schema: " + schema.Name + "\n")
	}

	var (
		shiftwidth int
		maxWidth   int
	)
	batch = api.NewBatch()
	batch.SetCurrentWindow(s.displayWin)
	batch.SetCurrentBuffer(s.displayBuf)
	batch.Call("shiftwidth", &shiftwidth)
	batch.WindowWidth(s.displayWin, &maxWidth)
	if err := batch.Execute(); err != nil {
		return err
	}

	batch = api.NewBatch()
	batch.SetBufferOption(s.displayBuf, "modifiable", true)
	batch.Command("%d")
	longestLine := s.drawSchemas(batch, shiftwidth)
	batch.SetBufferOption(s.displayBuf, "modifiable", false)
	// shrink window to fit, without growing
	if longestLine < maxWidth {
		batch.SetWindowWidth(s.displayWin, 8+longestLine) // minimum size of 8
	}
	return batch.Execute()
}

func (s *pluginState) refreshCache() error {
	schemaNames, err := s.db.ListSchemas()
	if err != nil {
		return err
	}
	log.Printf("schemas: %#v\n", schemaNames)

	cache := make([]schemaState, len(schemaNames))
	for i, name := range schemaNames {
		schema := &cache[i]
		schema.Name = name
		tables, err := s.db.ListTables(name)
		if err != nil {
			return err
		}

		schema.Tables = make([]dbman.TableSchema, len(tables))
		for i, name := range tables {
			tableSchema, err := s.db.DescribeTable(name)
			if err != nil {
				return err
			}
			schema.Tables[i] = *tableSchema
		}
	}
	s.displayCache[s.db.CurrentName()] = cache
	return nil
}

func (s *pluginState) drawSchemas(batch *nvim.Batch, shiftwidth int) int {
	schemas := s.displayCache[s.db.CurrentName()]

	var (
		sb strings.Builder

		// indenting for foldmethod=indent
		// schema name indent is 0
		tableNameFormat = strings.Repeat(" ", shiftwidth) + "%s\n"
		tableColFormat  = strings.Repeat(" ", shiftwidth*2) + "%s\t %s\t %s\n"

		longestLine int
	)

	var descWriter tabwriter.Writer

	for _, schema := range schemas {
		fmt.Fprintln(&sb, schema.Name)

		for _, tbl := range schema.Tables {
			fmt.Fprintf(&sb, tableNameFormat, tbl.Name)

			descWriter.Init(&sb, 2, 2, 1, ' ', tabwriter.Debug)
			for _, col := range tbl.Columns {
				// might not be the exact line length, but good enough for our purposes (until it isn't)
				lineLen, _ := fmt.Fprintf(&descWriter, tableColFormat, col.Name, col.Type, strings.Join(col.Attrs, "; "))
				if lineLen > longestLine {
					longestLine = lineLen
				}
			}
			descWriter.Flush()
		}
	}

	lines := strings.Split(sb.String(), "\n")
	// chop off trailing empty line
	lines = lines[:len(lines)-1]
	// NOTE: because of setting param after=false, there will be an extra tailing line
	batch.Put(lines, "l", false, false)
	return longestLine + 8
}
