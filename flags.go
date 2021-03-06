package main

import (
	"github.com/pkg/errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"os"
	"path/filepath"
	"strings"
)

type customFlagSet interface {
	parse() error
	fSet() *flag.FlagSet
}

type buildFlagSet struct {
	goBuildFlags *string
	only *string
	include *[]string
	exclude *[]string
	codeOutDir *string
	skipStrings *bool
	flagSet *flag.FlagSet
}

func(f *buildFlagSet) fSet() *flag.FlagSet {
	return f.flagSet
}

func (f *buildFlagSet) parse() error {

	// If you come back to this, and look at it, and think it could get run twice, and you don't want to
	// set env variables more than once, just know that it can't get run twice, because this method
	// gets run inside of a switch statement in the mainErr func. Tripped myself up here once.

	err := f.flagSet.Parse(os.Args[1:])
	if err != nil {
		return fmt.Errorf("Failed to parse args. Err: ", err)
	}

	if *f.codeOutDir != "" {
		*f.codeOutDir, err = filepath.Abs(*f.codeOutDir)
		if err != nil {
			return fmt.Errorf("Failed to get absolute path for code-out-dir flag. err: ", err)
		}
	}

	err = f.toEnv()
	if err != nil {
		return err
	}

	return nil
}

// Set build flags in environment so that they will be available when the app is called by toolexec
func (f *buildFlagSet) toEnv() error {

	fmt.Println("ONLY: ", *f.only)

	err := os.Setenv("ONLY", *f.only)
	if err != nil {
		return err
	}

	err = os.Setenv("CODE_OUT_DIR", *f.codeOutDir)
	if err != nil {
		return err
	}

	includeStr := strings.Join(*f.include, ",")
	err = os.Setenv("INCLUDE", includeStr)
	if err != nil {
		return err
	}

	excludeStr := strings.Join(*f.exclude, ",")
	err = os.Setenv("EXCLUDE", excludeStr)
	if err != nil {
		return err
	}

	var skipStrings string
	if *f.skipStrings {
		skipStrings = "TRUE"
	} else {
		skipStrings = "FALSE"
	}
	err = os.Setenv("SKIP_STRINGS", skipStrings)
	if err != nil {
		return err
	}

	return nil
}

func newBuildFlagSet() *buildFlagSet {
	fSet := flag.NewFlagSet("garble", flag.ExitOnError)

	flagSet := buildFlagSet{flagSet: fSet}

	flagSet.goBuildFlags = fSet.String("go-build-flags", "", "A string of flags " +
		"(wrapped in single quotes) to be passed to the 'go build' command.")

	flagSet.only = fSet.String("only", "", "Accepts a package name. " +
		"Only the package and it's subpackages will be garbled. ")

	flagSet.include = new([]string)
	fSet.StringArrayVar(flagSet.include, "include", []string{}, "Use with top level packages that don't have a . in the import name." +
		" For example, if a go.mod module is named myPackage instead of github.com/me/myPackage, it would not be garbled by default.")

	flagSet.exclude = new([]string)
	fSet.StringArrayVar(flagSet.exclude, "exclude", []string{}, "Accepts a package name. The package will not be garbled. " +
		"May be used multiple times to exclude multiple packages.")

	flagSet.codeOutDir = fSet.String("code-out-dir", "", "Directory to output garbled code for inspection.")

	flagSet.skipStrings = fSet.Bool("skip-strings", false, "set this flag if you don't want to obfuscate strings.")
	fSet.Lookup("skip-strings").NoOptDefVal = "true" // if they don't pass a value but they pass the flag, set to true

	fSet.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Usage of garble build:

	garble build [package] [flags]

Which is equivalent to the longer:

	go build -a -trimpath -toolexec=garble [build flags] [package]

The [build flags] referred to above are garble specific flags. To pass
flags to the 'go build' command, use the flag 'go-build-flags'

All packages except for standard library packages are garbled by default.

Standard library code is never garbled.

`[1:])

		fSet.PrintDefaults()
		os.Exit(2)
	}

	return &flagSet
}

type ungarbleFlagSet struct {
	sourcePath *string
	salt *string
	logPath *string
	outputPath *string
	flagSet *flag.FlagSet
}

func(f *ungarbleFlagSet) fSet() *flag.FlagSet {
	return f.flagSet
}

func (f *ungarbleFlagSet) parse() error {

	err := f.flagSet.Parse(os.Args[1:])
	if err != nil {
		return err
	}

	if *f.salt == "" {
		return errors.New("Salt flag must be set")

	}

	if *f.logPath == "" {
		return errors.New("Log path flag must be set")

	} else {
		*f.logPath, err = filepath.Abs(*f.logPath)
		if err != nil {
			return fmt.Errorf("Problem getting absolute path of log path: ", err)
		}

	}

	if *f.outputPath == "" {
		// if not supplied, use working dir
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Output flag was not passed, and couldn't determine working directory: ", err)
		}

		outputPath := filepath.Join(wd, "ungarbled_log.txt")

		*f.outputPath = outputPath

	} else {
		*f.outputPath, err = filepath.Abs(*f.sourcePath)
		if err != nil {
			return errors.Wrap(err, "Could not create absolute path from output path flag")
		}
	}

	if *f.sourcePath !=  ""{
		*f.sourcePath, err = filepath.Abs(*f.sourcePath)
		if err != nil {
			return errors.Wrap(err, "Could not create absolute path for source path flag")
		}
	}

	return nil
}

func newUngarbleFlagSet() *ungarbleFlagSet {
	fSet := flag.NewFlagSet("ungarble", flag.ExitOnError)

	flagSet := ungarbleFlagSet{flagSet: fSet}

	flagSet.sourcePath = fSet.String("source-path", "", "path to the original source that will be used for ungarbling.")
	flagSet.salt = fSet.String("salt", "", "the salt used for hashing, that was output to salt.txt," +
		" when you originally garbled the code.")
	flagSet.logPath = fSet.String("log-path", "", "path to the log file.")

	flagSet.outputPath = fSet.String("output-path", "", "path where you want the ungarbled log to be written" +
		" Defaults to the current working directory")

	fSet.Usage = func() {
		fmt.Fprintf(os.Stderr, "\ngarble ungarble [ungarble flags]\n\n")
		fSet.PrintDefaults()
		os.Exit(2)
	}

	return &flagSet
}




