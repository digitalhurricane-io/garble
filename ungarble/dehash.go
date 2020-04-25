package ungarble

import (

	"errors"
	"golang.org/x/tools/go/ast/astutil"
	"go/types"
	"mvdan.cc/garble/hashing"
	"strings"

	//"golang.org/x/tools/go/ast/astutil"
	"go/parser"
	"go/ast"
	"path/filepath"
	"os"
	"log"
)

// remember to add .go to filenames before hashing when trying to find in map

type fileInfo struct {
	name string
	path string
	file *ast.File
	typesInfo *types.Info
}

// The key will be the hash sha256(pacakge name + file name)
var hashToFileInfo =  make(map[string]fileInfo)

// key is hash, value is original method or function name.
// just used as a cache
var hashToIdentifierCache =  make(map[string]string)

// walk the given directory populating hashToFileInfo map
func populateFileHashInfo(salt string) error {

	var files []*ast.File

	err := filepath.Walk(originalSourcePath, func(path string, info os.FileInfo, err error) error {

		// make sure it's a go file
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// parse to *ast.File
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		files = append(files, file)

		hashedFileName := hashing.HashFileName(salt, info.Name(), file)

		typesInfo, err := getTypesInfo(path, file)
		if err != nil {
			return err
		}

		hashToFileInfo[hashedFileName] = fileInfo{
			info.Name(),
			path,
			file,
			typesInfo,
		}

		return nil
	})

	//log.Printf("%v", hashToFileInfo)

	return err
}

type garblePair struct {
	hashed string   // the hashed version we pull from the log file
	original string // the original string before it was hashed.
}

func getOriginalFileName(pair garblePair) garblePair {
	if pair.hashed == "" {
		return pair
	}

	if fileInfo, ok := hashToFileInfo[pair.hashed]; ok {
		pair.original = fileInfo.name
		return pair
	}

	// we didn't find the original name, so strings.Replace will just replace it with the same value
	pair.original = pair.hashed
	return pair
}

func getOriginalMethodName(pair garblePair, hashedFileName, salt string) (garblePair, error) {
	if pair.hashed == "" {
		return pair, nil
	}

	// first check cache
	if original, ok := hashToIdentifierCache[pair.hashed]; ok {
		pair.original = original
		return pair, nil
	}

	fileInfo, ok := hashToFileInfo[hashedFileName]
	if !ok {
		return pair, errors.New("Passed hashed filename was not found in map")
	}

	originalIdentifier := findOriginalIdentifier(fileInfo, pair.hashed, salt)

	pair.original = originalIdentifier

	return pair, nil
}

// Go through file and find function names.
// Then hash the function name and put values in cache.
// Return the identifier name if found
func findOriginalIdentifier(fileInfo fileInfo, hashedName, salt string) string {

	pre := func(cursor *astutil.Cursor) bool {

		switch node := cursor.Node().(type) {

			case *ast.Ident:

				if node.Name == "_" {
					return true // unnamed remains unnamed
				}

				if strings.HasPrefix(node.Name, "_C") || strings.Contains(node.Name, "_cgo") {
					return true // don't mess with cgo-generated code
				}

				obj := fileInfo.typesInfo.ObjectOf(node)

				switch x := obj.(type) {

					case *types.Func:
						_ = x
						funcName := node.Name
						hashedName := hashing.HashWith(salt, funcName)
						hashToIdentifierCache[hashedName] = funcName
				}

				return true
			}

		return true
	}

	_  = astutil.Apply(fileInfo.file, pre, nil).(*ast.File)

	if original, ok := hashToIdentifierCache[hashedName]; ok {
		return original
	}

	log.Println("Failed to find original value for function with hashed name: ", hashedName)
	// we didn't find it for some reason, so return the hashed version
	return hashedName
}