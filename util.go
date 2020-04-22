package main

import (
	"flag"
	"path/filepath"
)

func isGoFile(path string) bool {
	return filepath.Ext(path) == ".go"
}

