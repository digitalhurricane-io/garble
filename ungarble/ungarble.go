package ungarble

import (
	"go/importer"
	"go/token"
	"go/types"
	"path/filepath"

	//"math"
	//"time"
	"io/ioutil"
	"strings"
	//"strings"
	//"strings"
	"bufio"
	"io"
	"log"
	"os"

	//"github.com/pkg/errors"
	//"fmt"
	"regexp"
)

var (
	reFilename = regexp.MustCompile(`\s([a-zA-Z0-9_]+)\.go:\d`)
	reMethodName = regexp.MustCompile(`[a-zA-Z0-9]\.([a-zA-Z0-9_]+)[\(|\s]`)
	fileSet = token.NewFileSet()
	origTypesConfig = types.Config{Importer: importer.ForCompiler(fileSet, "gc", origLookup)}
	originalSourcePath = ""
)

func Ungarble(logFilePath, origSourcePath, salt, outputPath string) error {

	// set global
	originalSourcePath = origSourcePath

	// get needed info about all source files and put in a map
	err := populateFileHashInfo(salt)
	if err != nil {
		return err
	}

	// walk the log file looking for stacktraces, ungarbling them
	ungarbledContent, err := walkLog(logFilePath, salt)
	if err != nil {
		return err
	}

	// if outputPath not defined, use working directory
	if outputPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		outputPath = filepath.Join(wd, "ungarbled_log.txt")
	}

	writeFinalResult(outputPath, ungarbledContent)

	log.Println("Ungarbling successfully")

	return nil
}

// Reads the log file line by line, checking for stack traces 
// and replacing the hashed names with the original identifier names.
// Returns a string, does not edit the original log file.
func walkLog(filename, salt string) (string, error) {
    file, err := os.OpenFile(filename, os.O_RDWR, 0644)
    if err != nil {
		return "", err
    }
	defer file.Close()

	reader := bufio.NewReader(file)

	var previousLineContent string

	var newFileContent string

	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			break
		}
		if err != nil && err != io.EOF {
			return "", err
		}

		lineContent := string(line) + "\n" // add \n because ReadLine strips the \n
		//log.Println("Line content: ", lineContent)

		fileNamePair := garblePair{}
		
		methodNamePair := garblePair{}


		// This nesting is kind of ugly, but we only want to run one regex per line.
		// The filecheck regex is more reliable in not picking up any false positives.
		// So we backtrack and check the previous line for a method name if a filename was found.
		// The filename will be on the current line
		filenameFromLine := checkForFilename(lineContent)
		if filenameFromLine != "" {
			// line that was just read contained a hashed filename
			fileNamePair.hashed = filenameFromLine
			
			// now we want to check if the previous line has a method name, it should
			methodNameFromLine := checkForMethodName(previousLineContent)
			if methodNameFromLine != "" {
				// we have a hashed method name in the previous line
				methodNamePair.hashed = methodNameFromLine
			}
		}

		// Get original identifier names
		fileNamePair = getOriginalFileName(fileNamePair)
		methodNamePair, e := getOriginalMethodName(methodNamePair, fileNamePair.hashed, salt)
		if e != nil {
			return "", e
		}

		toWrite := previousLineContent

		previousLineContent = lineContent

		if fileNamePair.hashed != "" {
			previousLineContent = strings.Replace(lineContent, fileNamePair.hashed, fileNamePair.original, -1)
		}

		if methodNamePair.hashed != "" {
			toWrite = strings.Replace(toWrite, methodNamePair.hashed, methodNamePair.original, -1)
		}

		newFileContent += toWrite

		if err != nil {
			break
		}
	}

	newFileContent += previousLineContent

	return newFileContent, nil
}

// Check if string contains a filename, if so, return the filename without the extension
// The regex used assumes we're only passing in a line we know is part of a go stacktrace
func checkForFilename(line string) string {
	return checkForSubmatch(reFilename, line)
}

// Check if string contains a function or method name, if so, return the name without parenthesis.
// The regex used assumes we're only passing in a line we know is part of a go stacktrace
func checkForMethodName(line string) string {
	//log.Println(checkForSubmatch(reMethodName, line))
	return checkForSubmatch(reMethodName, line)
}

func checkForSubmatch(re *regexp.Regexp, text string) string {
	matchPair := re.FindSubmatch([]byte(text))
	if matchPair == nil || len(matchPair) < 2 {
		// no match or no submatch
		return ""
	}

	// wholeMatch := matchPair[0]
	subMatch := matchPair[1]
	
	return string(subMatch)
}

func writeFinalResult(fileName, content string) {
	err := ioutil.WriteFile(fileName, []byte(content), 0644)
	if err != nil {
		log.Println(err)
	}
}

