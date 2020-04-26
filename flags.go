package main

import (
	"errors"
	"fmt"
	flag "github.com/spf13/pflag"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type buildFlagSet struct {
	goBuildFlags *string
	only *string
	include *[]string
	exclude *[]string
	codeOutDir *string
	skipStrings *bool
	flagSet *flag.FlagSet
}

func (f *buildFlagSet) parse() error {

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

func newBuildFlagSet() buildFlagSet {
	fSet := flag.NewFlagSet("garble", flag.ExitOnError)

	flagSet := buildFlagSet{flagSet: fSet}

	flagSet.goBuildFlags = fSet.String("go-build-flags", "", "A string of flags " +
		"(wrapped in single quotes) to be passed to the 'go build' command.")

	flagSet.only = fSet.String("only", "", "Accepts a package name. " +
		"Only the passed package and it's subpackages will be garbled. " +
		"Can be used in combination with the 'include' and 'exclude' flag")

	flagSet.include = new([]string)
	fSet.StringArrayVar(flagSet.include, "include", []string{}, "Accepts a package name. The package will be garbled. " +
		"This flag is useful to ensure a package is not accidentaly skipped." +
		" It is also useful in combination with the 'only' flag. May be used multiple times to include multiple packages.")

	flagSet.exclude = new([]string)
	fSet.StringArrayVar(flagSet.exclude, "exclude", []string{}, "Accepts a package name. The package will not be garbled. " +
		"May be used multiple times to exclude multiple packages")

	flagSet.codeOutDir = fSet.String("code-out-dir", "", "Path to directory to output garbled code for inspection.")

	flagSet.skipStrings = fSet.Bool("skip-strings", false, "set this flag if you don't want to obfuscate strings.")
	flag.Lookup("skip-strings").NoOptDefVal = "true" // if they don't pass a value but they pass the flag, set to true

	fSet.Usage = func() {
		fmt.Fprintf(os.Stderr, `
Usage of garble build:

	garble build [build flags] [packages]

Which is equivalent to the longer:

	go build -a -trimpath -toolexec=garble [build flags] [packages]

The [build flags] referred to above are garble specific flags. To pass
flags to the 'go build' command, use the garble build flag 'go-build-flags'

All packages except for standard library packages are garbled by default. Package selection
for garbling is still not perfect. You can pass a package using the 'include' flag to ensure
a certain package (for example, your project code) is garbled.

Standard library code is never garbled.

`[1:])

		fSet.PrintDefaults()
		os.Exit(2)
	}

	return flagSet
}

type ungarbleFlagSet struct {
	sourcePath *string
	salt *string
	logPath *string
	outputPath *string
	flagSet *flag.FlagSet
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
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Output flag was not passed, and couldn't determine working directory: ", err)
		}
		*f.outputPath = wd
	}

	return nil
}

func newUngarbleFlagSet() (ungarbleFlagSet) {
	fSet := flag.NewFlagSet("ungarble", flag.ExitOnError)

	flagSet := ungarbleFlagSet{flagSet: fSet}

	flagSet.sourcePath = flag.String("source-path", "", "path to the original source that will be used for ungarbling.")
	flagSet.salt = fSet.String("salt", "", "the salt used for hashing, that was output to salt.txt," +
		" when you originally garbled the code.")
	flagSet.logPath = fSet.String("log-path", "", "path to the log file.")

	flagSet.outputPath = fSet.String("output-path", "", "path where you want the ungarbled log to be written")

	fSet.Usage = func() {
		fmt.Fprintf(os.Stderr, "\ngarble ungarble [ungarble flags]\n\n")
		fSet.PrintDefaults()
		os.Exit(2)
	}

	return flagSet
}

// Sets flags provided to garble as environmental variables so that they will
// be available when go build is run.
// A flag name of "code-outdir" becomes and env variable of "CODE_OUTDIR"
func buildFlagsToEnv(fSet *flag.FlagSet) error {
	var outsideErr error

	fSet.Visit(func(f *flag.Flag) {
		flagName := f.Name
		flagVal := f.Value.

		// make sure path supplied is an absolute path
		switch flagName {

		case "include":
			fallthrough
		case "exclude":


		case "code-outdir":
			var err error
			flagVal, err = filepath.Abs(flagVal)
			if err != nil {
				outsideErr = err
				return
			}
		}

		envVarName := strings.ToUpper(strings.Replace(f.Name, "-", "_", -1))

		err := os.Setenv(envVarName, flagVal)
		if err != nil {
			outsideErr = err
		}
	})

	if outsideErr != nil {
		log.Println(outsideErr)
	}

	return outsideErr
}

func ungarbleFlagsToEnv() {

}


