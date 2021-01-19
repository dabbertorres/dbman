package main

import (
	"bytes"
	"fmt"
	"log"
	"regexp"
	"strconv"
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
		p.HandleFunction(listConnectionsCompletion(&state))
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
		Name:  "DBDescribe",
		NArgs: "1",
		Bar:   true,
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
			if err := state.displaySchemas(api, true); err != nil {
				return err
			}
		}

		api.WriteOut(fmt.Sprintf("connected to '%s'!\n", args[0]))
		return nil
	}
}

func refreshSchema(state *pluginState) (*plugin.CommandOptions, func(*nvim.Nvim) error) {
	opts := &plugin.CommandOptions{
		Name:  "DBRefresh",
		NArgs: "0",
	}
	return opts, func(api *nvim.Nvim) error {
		if err := state.displaySchemas(api, true); err != nil {
			api.WriteErr("failed to update and display schema: " + err.Error())
		}
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
			targetBuffer nvim.Buffer
			targetWindow nvim.Window
			reusing      bool // track if we should clear the buffer
		)

		// did the user specify a buffer?
		if len(args) != 0 {
			buf, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			targetBuffer = nvim.Buffer(buf)
			reusing = true

			// if the buffer we're targeting is already in an open window,
			// use that window - otherwise, open a new window
			visible, win, err := isBufferVisible(api, targetBuffer)
			if err != nil {
				return err
			}

			if visible {
				targetWindow = win
			}
		}

		if targetWindow == 0 {
			targetBuffer, _, err = openSplitWindow(api, true, targetBuffer)
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
		_ = api.SetBufferName(targetBuffer, fmt.Sprintf("[%s] %s", state.db.CurrentName(), query))

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

		maxWidth, err := api.WindowWidth(state.displayWin)
		if err != nil {
			api.WriteErr("failed to obtain window width: " + err.Error())
			return nil
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
			// add 8 (hopefully big enough) to account for Window padding (e.g. gutter)
			if err := api.SetWindowWidth(state.displayWin, 8+maxLineLen); err != nil {
				api.WriteErr("failed to set window width: " + err.Error())
			}
		}

		autoDisplay := true
		_ = api.Var("db_auto_display_schema", &autoDisplay)
		if autoDisplay {
			// _very_ simple attempt at detecting if the schema display needs refreshing
			if matched, _ := regexp.MatchString(` table `, strings.ToLower(query)); matched {
				go func() {
					if err := state.displaySchemas(api, true); err != nil {
						api.WriteErr("failed to update schema display: " + err.Error())
					}
				}()
			}
		}

		return nil
	}
}
