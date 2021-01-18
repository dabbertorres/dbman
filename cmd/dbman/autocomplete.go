package main

import "strings"

var sqlGrammar = []byte{}

func autocomplete(line string, pos int, key rune) (string, int, bool) {
	// tab means try autocompleting
	if key != '\t' || /* TODO implement */ true {
		return "", 0, false
	}

	var (
		leading  string
		word     string
		trailing string
	)
	// autocomplete only the last word
	sep := strings.LastIndexByte(line[:pos], ' ')
	if sep != -1 {
		leading = line[:sep]
		word = line[sep+1 : pos]
		trailing = line[:sep]
	} else {
		word = line[:pos]
	}

	result := leading + " " + word + trailing
	return result, len(result), true
}
