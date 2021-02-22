package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"text/tabwriter"

	"dabbertorres.dev/dbman"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

type cli struct {
	terminal *term.Terminal
	db       *dbman.DBMan
	prompter ssh.KeyboardInteractiveChallenge
	running  bool
}

func newCLI(terminal *term.Terminal, db *dbman.DBMan) *cli {
	return &cli{
		terminal: terminal,
		db:       db,
		prompter: dbman.PasswordPrompt(terminal),
		running:  true,
	}
}

func (c *cli) Close() error {
	return c.db.Close()
}

func (c *cli) println(args ...interface{}) {
	fmt.Fprintln(c.terminal, args...)
}

func (c *cli) printf(format string, args ...interface{}) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	fmt.Fprintf(c.terminal, format, args...)
}

func (c *cli) run(initialConnection string) {
	defer c.Close()

	if initialConnection != "" {
		if err := c.db.SwitchConnection(initialConnection, c.prompter); err != nil {
			log.Fatal(err)
		}
	}

	for c.running {
		line, err := c.terminal.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			if err != term.ErrPasteIndicator {
				c.println(err)
				continue
			}
		}
		line = strings.TrimSpace(line)

		if line[0] == '\\' {
			args := strings.Split(line[1:], " ")
			for i := 0; i < len(args); i++ {
				arg := strings.TrimSpace(args[i])
				if arg == "" {
					args = append(args[:i], args[i+1:]...)
					i--
				} else {
					args[i] = arg
				}
			}

			if err := c.command(args); err != nil {
				c.println(err)
			}
		} else {
			if err := c.query(line); err != nil {
				c.println(err)
			}
		}
	}
}

func (c *cli) command(args []string) error {
	switch args[0] {
	case "active", "a":
		return c.activeConnection(args[1:])

	case "connections", "c":
		return c.listConnections(args[1:])

	case "switch", "s":
		return c.switchConnection(args[1:])

	case "tables", "t":
		return c.listTables(args[1:])

	case "schemas", "sn":
		return c.listSchemas(args[1:])

	case "describe", "d":
		return c.describeTable(args[1:])

	case "stats":
		return c.printStats(args[1:])

	case "help", "h", "?":
		c.help()
		return nil

	case "quit", "q":
		c.running = false
		return nil

	default:
		return errors.New("unknown command")
	}
}

func (c *cli) help() {
	c.println("commands:")
	c.println()
	c.println(`Connections:`)
	c.println(`\active (\a): print the name of the active database connection.`)
	c.println(`\connections (\c): print a list of available database connections.`)
	c.println(`\switch (\s): switch to (and open if necessary) a different connection.`)
	c.println()
	c.println(`Database:`)
	c.println(`\tables (\t): print a list of accessible tables. An (optional) schema name may be provided, otherwise the public schema is used.`)
	c.println(`\schemas (\sn): print a list of accessible schemas (if relevant for current connection).`)
	c.println(`\describe (\d): print the schema of a given table. To specify a non-public table, use <schema>.<table> syntax.`)
	c.println()
	c.println(`Extra:`)
	c.println(`\stats: print stats about the current database connection`)
	c.println(`\help (\h, \?): print this dialog.`)
	c.println(`\quit (\q): exit.`)
	c.println()
}

func (c *cli) activeConnection(args []string) error {
	if c.db.CurrentName() != "" {
		c.println(c.db.CurrentName())
	} else {
		c.println("no active connection")
	}
	return nil
}

func (c *cli) listConnections(args []string) error {
	names, active := c.db.ListConnections()
	for i := range names {
		// denote open connections
		if active[i] {
			c.println(names[i], " *")
		} else {
			c.println(names[i])
		}
	}
	return nil
}

func (c *cli) switchConnection(args []string) error {
	if len(args) != 1 {
		return errors.New("a single connection name must be specified")
	}

	return c.db.SwitchConnection(args[0], c.prompter)
}

func (c *cli) listTables(args []string) error {
	var schema string
	if len(args) != 0 {
		schema = args[0]
	}

	tables, err := c.db.ListTables(schema)
	if err != nil {
		return err
	}

	for _, name := range tables {
		c.println(name)
	}

	return nil
}

func (c *cli) listSchemas(args []string) error {
	schemas, err := c.db.ListSchemas()
	if err != nil {
		return err
	}

	for _, name := range schemas {
		c.println(name)
	}

	return nil
}

func (c *cli) describeTable(args []string) error {
	if len(args) < 1 {
		return errors.New("'\\describe' requires at least one table name")
	}

	for _, name := range args {
		schema, err := c.db.DescribeTable(name)
		if err != nil {
			return err
		}

		c.println(schema.Name)
		writer := tabwriter.NewWriter(c.terminal, 2, 2, 1, ' ', tabwriter.Debug)
		for _, col := range schema.Columns {
			fmt.Fprintf(writer, " %s\t %s\t %s\n", col.Name, col.Type, strings.Join(col.Attrs, "; "))
		}
		writer.Flush()
		c.println()
	}

	return nil
}

func (c *cli) printStats(args []string) error {
	// ignore arguments
	stats := c.db.Stats()

	c.println("Connections")
	c.printf("Open:             % 9d", stats.OpenConnections)
	c.printf("In Use:           % 9d", stats.InUse)
	c.printf("Idle:             % 9d", stats.Idle)
	c.printf("Idle Closed:      % 9d", stats.MaxIdleClosed)
	c.printf("Idle Time Closed: % 9d", stats.MaxIdleTimeClosed)
	c.printf("Lifetime Closed:  % 9d", stats.MaxLifetimeClosed)
	c.println()
	c.println("Wait Counters:")
	c.printf("Count:            % 9d", stats.WaitCount)
	c.printf("Total Duration:   % 9s", stats.WaitDuration)
	c.println()

	return nil
}

func (c *cli) query(line string) error {
	result, err := c.db.Query(line)
	if err != nil {
		return err
	}

	marks := make([]string, len(result.Columns))
	for i := range marks {
		marks[i] = " %s"
	}
	printFmt := strings.Join(marks, "\t") + "\n"

	writer := tabwriter.NewWriter(c.terminal, 2, 2, 1, ' ', tabwriter.Debug)

	colNames := make([]interface{}, len(result.Columns))
	for i, col := range result.Columns {
		colNames[i] = col
	}
	length, _ := fmt.Fprintf(writer, printFmt, colNames...)
	fmt.Fprintln(writer, strings.Repeat("-", length))

	for _, row := range result.Rows {
		fmt.Fprintf(writer, printFmt, row...)
	}

	// don't want to write anything until we're done (and successful)
	return writer.Flush()
}
