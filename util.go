package main

import (
	"flag"
	"path/filepath"
)

func isGoFile(path string) bool {
	return filepath.Ext(path) == ".go"
}

// Checks whether the user actually passed the flag.
// The flag has a value regardless of whether the user actually passed it,
// because of the default. Thus the need for this function.
func wasFlagPassed(flagSet *flag.FlagSet, name string) bool {
	found := false
	flagSet.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}