package main

import (
	"bytes"
	"fmt"
	"log"
	"regexp"
	"strings"
	"text/tabwriter"

	"dabbertorres.dev/dbman"
	"github.com/neovim/go-client/nvim"
	"github.com/neovim/go-client/nvim/plugin"
)

func main() {
	var cfg dbman.Config
	if err := dbman.LoadConfig(dbman.DefaultConfigFile, true, &cfg); err != nil {
		log.Print(err)
	}

	db := dbman.New(&cfg)
	defer db.Close()
	state := pluginState{
		db:           db,
		displayBuf:   -1,
		displayWin:   -1,
		displayCache: make(map[string][]schemaState),
	}

	plugin.Main(func(p *plugin.Plugin) error {
		p.HandleFunction(listConnectionsFunc(&state))
		p.HandleFunction(listTablesFunc(&state))

		p.HandleCommand(listConnections(&state))
		p.HandleCommand(listSchemas(&state))
		p.HandleCommand(listTables(&state))
		p.HandleCommand(describeTable(&state))
		p.HandleCommand(switchConnection(&state))
		p.HandleCommand(refreshSchema(&state))
		p.HandleCommand(runQuery(&state))
		return nil
	})
}

func listConnectionsFunc(state *pluginState) (*plugin.FunctionOptions, func(*nvim.Nvim, []interface{}) (string, error)) {
	opts := &plugin.FunctionOptions{
		Name: "DBConnections",
	}
	return opts, func(*nvim.Nvim, []interface{}) (string, error) {
		names, _ := state.db.ListConnections()
		return strings.Join(names, "\n") + "\n", nil
	}
}

func listTablesFunc(state *pluginState) (*plugin.FunctionOptions, func(*nvim.Nvim, []interface{}) (string, error)) {
	opts := &plugin.FunctionOptions{
		Name: "DBTables",
	}
	return opts, func(*nvim.Nvim, []interface{}) (string, error) {
		cache, ok := state.displayCache[state.db.CurrentName()]
		if !ok {
			if err := state.refreshCache(); err != nil {
				return "", err
			}
			cache = state.displayCache[state.db.CurrentName()]
		}

		var numTables int
		for _, schema := range cache {
			numTables += len(schema.Tables)
		}
		tables := make([]string, numTables)

		var offset int
		for _, schema := range cache {
			for i, table := range schema.Tables {
				tables[offset+i] = table.Name
			}
			offset += len(schema.Tables)
		}

		return strings.Join(tables, "\n") + "\n", nil
	}
}

func listConnections(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBConnections",
		NArgs: "0",
		Bar:   true,
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
		Bar:   true,
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
		Bar:   true,
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
		Name:     "DBDescribe",
		NArgs:    "1",
		Bar:      true,
		Complete: "custom,DBTables",
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
		Complete: "custom,DBConnections",
	}
	return opts, func(api *nvim.Nvim, args []string) error {
		go func() {
			if err := state.db.SwitchConnection(strings.TrimSpace(args[0]), passwordPrompt(api)); err != nil {
				api.WritelnErr(fmt.Sprintf("failed to connect to '%s': %v", args[0], err))
				return
			}
			api.WriteOut(fmt.Sprintf("connected to '%s'!\n", args[0]))

			autoDisplay := true
			_ = api.Var("db_auto_display_schema", &autoDisplay)

			if autoDisplay {
				if err := state.displaySchemas(api, true); err != nil {
					api.WritelnErr("failed to display schema: " + err.Error())
				}
			}
		}()
		return nil
	}
}

func refreshSchema(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBRefresh",
		NArgs: "0",
	}
	return opts, func(api *nvim.Nvim) error {
		go func() {
			if err := state.displaySchemas(api, true); err != nil {
				api.WritelnErr("failed to update and display schema: " + err.Error())
			}
		}()
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
	return opts, func(api *nvim.Nvim, _ []string, bufRange [2]int) error {
		// grab the query
		queryBuffer, err := api.CurrentBuffer()
		if err != nil {
			return err
		}

		queryLines, err := api.BufferLines(queryBuffer, bufRange[0]-1, bufRange[1], false)
		if err != nil {
			return err
		}

		query := string(bytes.Join(queryLines, []byte{' '}))

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
			marks[i] = "%v"
		}
		printFmt := strings.Join(marks, " |\t") + "\t\n"

		var sb strings.Builder
		writer := tabwriter.NewWriter(&sb, 3, 4, 1, ' ', tabwriter.AlignRight)

		colNames := make([]interface{}, len(result.Columns))
		for i, col := range result.Columns {
			colNames[i] = col
		}
		fmt.Fprintf(writer, printFmt, colNames...)

		for _, row := range result.Rows {
			fmt.Fprintf(writer, printFmt, row...)
		}

		if err := writer.Flush(); err != nil {
			return err
		}

		if state.outputWin == 0 {
			state.outputBuf, state.outputWin, err = openSplitWindow(api, false, state.outputBuf)
			if err != nil {
				return err
			}
		}

		lines := strings.Split(sb.String(), "\n")

		// insert a divider
		lines = append(lines, "")
		copy(lines[2:], lines[1:])
		lines[1] = strings.Repeat("-", len(lines[0]))

		batch := api.NewBatch()
		batch.SetBufferName(state.outputBuf, fmt.Sprintf("[%s] %s", state.db.CurrentName(), query))
		batch.SetCurrentWindow(state.outputWin)
		batch.SetCurrentBuffer(state.outputBuf)
		batch.Command("%d")
		batch.Put(lines, "l", false, false)
		batch.SetWindowCursor(state.outputWin, [2]int{1, 1})
		if err := batch.Execute(); err != nil {
			return err
		}

		autoDisplay := true
		_ = api.Var("db_auto_display_schema", &autoDisplay)
		if autoDisplay {
			// _very_ simple attempt at detecting if the schema display needs refreshing
			if matched, _ := regexp.MatchString(` table `, strings.ToLower(query)); matched {
				go func() {
					if err := state.displaySchemas(api, true); err != nil {
						api.WritelnErr("failed to update schema display: " + err.Error())
					}
				}()
			}
		}

		return nil
	}
}
