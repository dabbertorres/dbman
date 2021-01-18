package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"dabbertorres.dev/dbman"
	"github.com/neovim/go-client/nvim"
	"github.com/neovim/go-client/nvim/plugin"
)

type pluginState struct {
	db           *dbman.DBMan
	displayBuf   nvim.Buffer
	displayWin   nvim.Window
	displayCache map[string][]schemaState // connection -> schemas -> tables
}

type schemaState struct {
	name   string
	tables []dbman.TableSchema
}

func main() {
	var cfg dbman.Config
	dbman.LoadConfig(dbman.DefaultConfigFile, true, &cfg)

	state := pluginState{
		db:           dbman.New(&cfg),
		displayBuf:   -1,
		displayWin:   -1,
		displayCache: make(map[string][]schemaState),
	}
	defer state.db.Close()

	plugin.Main(func(p *plugin.Plugin) error {
		p.HandleFunction(listConnectionsCompletion(&state))
		p.HandleCommand(listConnections(&state))
		p.HandleCommand(listSchemas(&state))
		p.HandleCommand(listTables(&state))
		p.HandleCommand(describeTable(&state))
		p.HandleCommand(switchConnection(&state))
		p.HandleCommand(runQuery(&state))
		return nil
	})
}

func listConnectionsCompletion(state *pluginState) (*plugin.FunctionOptions, func(*nvim.Nvim, []interface{}) (string, error)) {
	opts := &plugin.FunctionOptions{
		Name: "DBConnectionsF",
	}
	return opts, func(*nvim.Nvim, []interface{}) (string, error) {
		names, _ := state.db.ListConnections()
		return strings.Join(names, "\n") + "\n", nil
	}
}

func listConnections(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBConnections",
		NArgs: "0",
	}
	return opts, func(api *nvim.Nvim) error {
		names, _ := state.db.ListConnections()
		return api.WriteOut(strings.Join(names, "\n") + "\n")
	}
}

func listSchemas(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBSchemas",
		NArgs: "0",
	}

	return opts, func(api *nvim.Nvim) error {
		schemas, _ := state.db.ListSchemas()
		return api.WriteOut(strings.Join(schemas, "\n") + "\n")
	}
}

func listTables(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim, []string) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBTables",
		NArgs: "*",
	}

	return opts, func(api *nvim.Nvim, args []string) error {
		var tables []string
		switch len(args) {
		case 0:
			var err error
			tables, err = state.db.ListTables("")
			if err != nil {
				return err
			}

		case 1:
			var err error
			tables, err = state.db.ListTables(args[0])
			if err != nil {
				return err
			}

		default:
			for _, schema := range args {
				schemaTables, err := state.db.ListTables(schema)
				if err != nil {
					return err
				}

				// prefix with schema names
				for i, t := range schemaTables {
					schemaTables[i] = schema + "." + t
				}
				tables = append(tables, schemaTables...)
			}
		}
		return api.WriteOut(strings.Join(tables, "\n") + "\n")
	}
}

func describeTable(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim, []string) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBDescribe",
		NArgs: "1",
	}
	return opts, func(api *nvim.Nvim, args []string) error {
		table := strings.TrimSpace(args[0])
		schema, err := state.db.DescribeTable(table)
		if err != nil {
			return err
		}

		var sb strings.Builder
		writer := tabwriter.NewWriter(&sb, 2, 2, 1, ' ', tabwriter.Debug)
		for _, col := range schema.Columns {
			fmt.Fprintf(&sb, " %s\t %s\t %s\n", col.Name, col.Type, strings.Join(col.Attrs, "; "))
		}
		writer.Flush()
		return api.WriteOut(sb.String())
	}
}

func switchConnection(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim, []string) error) {
	opts := &plugin.CommandOptions{
		Name:     "DBConnect",
		NArgs:    "1",
		Complete: "custom,DBConnectionsF",
	}
	return opts, func(api *nvim.Nvim, args []string) error {
		if err := state.db.SwitchConnection(strings.TrimSpace(args[0]), passwordPrompt(api)); err != nil {
			api.WritelnErr(fmt.Sprintf("failed to connect to '%s': %v", args[0], err))
			return err
		}

		autoDisplay := true
		_ = api.Var("db_auto_display_schema", &autoDisplay)

		if autoDisplay {
			if err := state.setupDisplay(api, true); err != nil {
				return err
			}
		}

		api.WriteOut(fmt.Sprintf("connected to '%s'!\n", args[0]))
		return nil
	}
}

func runQuery(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim, []string, [2]int) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBRun",
		NArgs: "?",
		Range: "%",
		Addr:  "lines",
		Bar:   true,
	}
	return opts, func(api *nvim.Nvim, args []string, bufRange [2]int) error {
		// grab the query
		queryBuffer, err := api.CurrentBuffer()
		if err != nil {
			return err
		}

		queryLines, err := api.BufferLines(queryBuffer, bufRange[0]-1, bufRange[1], false)
		if err != nil {
			return err
		}

		query := string(bytes.Join(queryLines, []byte{}))

		// run it!
		result, err := state.db.Query(query)
		if err != nil {
			return err
		}

		if result == nil || len(result.Columns) == 0 {
			api.WriteOut("no results\n")
			return nil
		}

		// format it!
		marks := make([]string, len(result.Columns))
		for i := range marks {
			marks[i] = " %v"
		}
		printFmt := strings.Join(marks, "\t") + "\n"

		var sb strings.Builder
		writer := tabwriter.NewWriter(&sb, 2, 2, 1, ' ', tabwriter.Debug)

		colNames := make([]interface{}, len(result.Columns))
		for i, col := range result.Columns {
			colNames[i] = col
		}
		length, _ := fmt.Fprintf(writer, printFmt, colNames...)
		fmt.Fprintln(writer, strings.Repeat("-", length))

		for _, row := range result.Rows {
			fmt.Fprintf(writer, printFmt, row...)
		}

		if err := writer.Flush(); err != nil {
			return err
		}

		var (
			buffer       nvim.Buffer
			reusing      bool // track if we should clear the buffer
			targetWindow nvim.Window
		)

		// did the user specify a buffer?
		if len(args) != 0 {
			buf, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			buffer = nvim.Buffer(buf)
			reusing = true

			// if the buffer we're targeting is already in an open window,
			// use that window - otherwise, open a new window

			tabpage, err := api.CurrentTabpage()
			if err != nil {
				return err
			}

			windows, err := api.TabpageWindows(tabpage)
			if err != nil {
				return err
			}

			for _, win := range windows {
				buf, err := api.WindowBuffer(win)
				if err != nil {
					return err
				}
				if buf == buffer {
					targetWindow = win
					break
				}
			}
		}

		if targetWindow == 0 {
			buffer, _, err = openSplitWindow(api, true, buffer)
			if err != nil {
				return err
			}
		} else {
			if err := api.SetCurrentWindow(targetWindow); err != nil {
				return err
			}
		}

		// set the buffer's name to '[connection] query'
		// if it fails, oh well, doesn't hurt anything
		_ = api.SetBufferName(buffer, fmt.Sprintf("[%s] %s", state.db.CurrentName, query))

		if reusing {
			// may need to clear the buffer of any previous contents
			if err := api.Command("%d"); err != nil {
				return err
			}
		}

		lines := strings.Split(sb.String(), "\n")
		if err := api.Put(lines, "l", false, false); err != nil {
			return err
		}

		maxWidth, err := api.WindowWidth(0)
		if err != nil {
			return err
		}

		// shrink window to fit
		maxLineLen := len(lines[0])
		for _, l := range lines[1:] {
			lineLen := len(l)
			if lineLen > maxLineLen {
				maxLineLen = lineLen
			}
		}

		// but don't grow the window
		if maxLineLen < maxWidth {
			_ = api.SetWindowWidth(0, 8+maxLineLen) // minimum size of 8
		}

		return nil
	}
}

func (s *pluginState) setupDisplay(api *nvim.Nvim, newConn bool) error {
	var (
		validBuf bool
		validWin bool
	)
	batch := api.NewBatch()
	if s.displayBuf > 0 {
		batch.IsBufferLoaded(s.displayBuf, &validBuf)
	}
	if s.displayWin > 0 {
		batch.IsWindowValid(s.displayWin, &validWin)
	}
	if err := batch.Execute(); err != nil {
		return err
	}

	var updateDisplays bool

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

		// set the buffer's name to 'connection'
		// if it fails, oh well, doesn't hurt anything
		_ = api.SetBufferName(s.displayBuf, s.db.CurrentName)

		batch := api.NewBatch()
		batch.SetWindowOption(0, "foldenable", true)
		batch.SetWindowOption(0, "foldmethod", "indent")
		batch.SetWindowOption(0, "foldlevel", 1)
		if err := batch.Execute(); err != nil {
			return err
		}
	}

	if newConn {
		schemas, err := s.db.ListSchemas()
		if err != nil {
			return err
		}

		cache := make([]schemaState, len(schemas))
		for i, schema := range schemas {
			cache[i].name = schema
		}
		s.displayCache[s.db.CurrentName] = cache

		updateDisplays = true
	}

	if updateDisplays {
		var (
			currWin nvim.Window
			currBuf nvim.Buffer
		)

		batch := api.NewBatch()
		batch.CurrentWindow(&currWin)
		batch.CurrentBuffer(&currBuf)
		if err := batch.Execute(); err != nil {
			return err
		}

		var shiftwidth int
		if err := api.Call("shiftwidth", &shiftwidth); err != nil {
			return err
		}

		maxWidth, err := api.WindowWidth(0)
		if err != nil {
			return err
		}

		batch = api.NewBatch()
		batch.SetCurrentWindow(s.displayWin)
		batch.SetCurrentBuffer(s.displayBuf)
		batch.SetBufferOption(s.displayBuf, "modifiable", true)
		if err := s.displaySchemas(batch, shiftwidth, maxWidth); err != nil {
			return err
		}
		batch.SetBufferOption(s.displayBuf, "modifiable", false)
		batch.SetCurrentWindow(currWin)
		batch.SetCurrentBuffer(currBuf)
		if err := batch.Execute(); err != nil {
			return err
		}
	}

	return nil
}

func (s *pluginState) displaySchemas(batch *nvim.Batch, shiftwidth, maxWidth int) error {
	schemas := s.displayCache[s.db.CurrentName]

	var (
		sb strings.Builder

		// indenting for foldmethod=indent
		// schema name indent is 0
		tableNameFormat = strings.Repeat(" ", shiftwidth) + "%s\n"
		tableColFormat  = strings.Repeat(" ", shiftwidth*2) + "%s\t %s\t %s\n"

		longestLine int
	)

	var descWriter tabwriter.Writer

	for i := range schemas {
		schema := &schemas[i]
		tables, err := s.db.ListTables(schema.name)
		if err != nil {
			return err
		}
		fmt.Fprintln(&sb, schema.name)

		schema.tables = make([]dbman.TableSchema, len(tables))
		for j, tbl := range tables {
			tblSchema, err := s.db.DescribeTable(schema.name + "." + tbl)
			if err != nil {
				return err
			}
			schema.tables[j] = *tblSchema
			fmt.Fprintf(&sb, tableNameFormat, tbl)

			descWriter.Init(&sb, 2, 2, 1, ' ', tabwriter.Debug)
			for _, col := range tblSchema.Columns {
				// not exact line length, but good enough for our purposes
				lineLen, _ := fmt.Fprintf(&descWriter, tableColFormat, col.Name, col.Type, strings.Join(col.Attrs, "; "))
				if lineLen > longestLine {
					longestLine = lineLen
				}
			}
			descWriter.Flush()
		}
	}

	batch.Put(strings.Split(sb.String(), "\n"), "l", false, false)

	// shrink window to fit, without growing
	if longestLine < maxWidth {
		batch.SetWindowWidth(0, 8+longestLine) // minimum size of 8
	}
	return nil
}

func passwordPrompt(api *nvim.Nvim) func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
	return func(_, _ string, questions []string, echos []bool) (answers []string, err error) {
		answers = make([]string, len(questions))
		batch := api.NewBatch()

		var outOfMem int
		batch.Call("inputsave", &outOfMem)
		for i, q := range questions {
			if echos[i] {
				batch.Call("input", &answers[i], q)
			} else {
				batch.Call("inputsecret", &answers[i], q)
			}
		}
		batch.Call("inputrestore", &outOfMem)

		if err := batch.Execute(); err != nil {
			return nil, err
		}
		if outOfMem != 0 {
			return nil, errors.New("ran out of memory")
		}
		return answers, nil
	}
}

// openSplitWindow provides a workaround for the lack of an RPC function
// for opening a non-floating window.
// If buf is non-zero, the identified buffer is opened in the new window.
func openSplitWindow(api *nvim.Nvim, vertical bool, buf nvim.Buffer) (nvim.Buffer, nvim.Window, error) {
	var cmd string

	if buf == 0 {
		if vertical {
			cmd = "vnew"
		} else {
			cmd = "new"
		}
	} else {
		if vertical {
			cmd = "vertical sbuffer"
		} else {
			cmd = "sbuffer"
		}
	}

	cmd += ` +echo\ printf('%s:%s',bufnr(),win_getid())`
	out, err := api.CommandOutput(cmd)
	if err != nil {
		return 0, 0, err
	}

	parts := strings.Split(out, ":")

	bufnr, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}

	winnr, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}

	prevBuf := buf
	buf = nvim.Buffer(bufnr)
	win := nvim.Window(winnr)

	if buf != prevBuf {
		batch := api.NewBatch()
		// scratch
		batch.SetBufferOption(buf, "buftype", "nofile")
		batch.SetBufferOption(buf, "bufhidden", "hide")
		batch.SetBufferOption(buf, "swapfile", false)
		// unlisted
		batch.SetBufferOption(buf, "buflisted", false)
		return buf, win, batch.Execute()
	}

	return buf, win, nil
}
