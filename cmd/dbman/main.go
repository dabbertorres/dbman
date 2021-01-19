package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"dabbertorres.dev/dbman"
	"golang.org/x/term"
)

func main() {
	log.SetFlags(0)

	var (
		configFile  string
		list        bool
		listDrivers bool
	)
	flag.StringVar(&configFile, "cfg", dbman.DefaultConfigFile, "specify a config file to use")
	flag.BoolVar(&list, "list", false, "list available connections")
	flag.BoolVar(&listDrivers, "list-drivers", false, "list available SQL drivers")
	flag.Parse()

	var cfg dbman.Config
	if err := dbman.LoadConfig(configFile, configFile == dbman.DefaultConfigFile, &cfg); err != nil {
		log.Fatal(err)
	}

	switch {
	case list:
		for k := range cfg.Connections {
			fmt.Println(k)
		}

	case listDrivers:
		for _, v := range sql.Drivers() {
			fmt.Println(v)
		}

	default:
		connName := flag.Arg(0)
		if _, ok := cfg.Connections[connName]; !ok {
			log.Fatalf("'%s' is not a configured connection", connName)
		}

		if !term.IsTerminal(0) {
			log.Fatal("an active terminal is required")
		}

		prevState, err := term.MakeRaw(0)
		if err != nil {
			log.Fatal("failed to enter terminal raw mode:", err)
		}
		defer term.Restore(0, prevState)

		terminal := term.NewTerminal(makeReadWriter(os.Stdin, os.Stdout), "> ")
		terminal.AutoCompleteCallback = autocomplete

		// just in case it is still set when we exit
		defer terminal.SetBracketedPasteMode(false)

		os.Stdin.Sync()

		db := dbman.New(&cfg)
		newCLI(terminal, db).run(connName)
	}
}

type combinedReaderWriter struct {
	io.Reader
	io.Writer
}

func makeReadWriter(r io.Reader, w io.Writer) *combinedReaderWriter {
	return &combinedReaderWriter{Reader: r, Writer: w}
}
