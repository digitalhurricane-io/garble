// Copyright (c) 2019, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/dchest/uniuri"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/ast/astutil"
	"io"
	"io/ioutil"
	"log"
	"mvdan.cc/garble/hashing"
	stringsG "mvdan.cc/garble/strings"
	"mvdan.cc/garble/ungarble"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() { os.Exit(main1()) }

var (
	deferred []func() error
	fset     = token.NewFileSet()

	printerConfig   = printer.Config{Mode: printer.RawFormat}
	origTypesConfig = types.Config{Importer: importer.ForCompiler(fset, "gc", origLookup)}

	buildInfo       = packageInfo{imports: make(map[string]importedPkg)}
	garbledImporter = importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(buildInfo.imports[path].packagefile)
	}).(types.ImporterFrom)
)

// origLookup helps implement a types.Importer which finds the export data for
// the original dependencies, not their garbled counterparts. This is useful to
// typecheck a package before it's garbled, so we can make decisions on how to
// garble it.
func origLookup(path string) (io.ReadCloser, error) {
	cmd := exec.Command("go", "list", "-json", "-export", path)
	dir := os.Getenv("GARBLE_DIR")
	if dir == "" {
		return nil, fmt.Errorf("$GARBLE_DIR unset; did you run via 'garble build'?")
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go list error: %v: %s", err, out)
	}
	var res struct {
		Export string
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	return os.Open(res.Export)
}

func garbledImport(path string) (*types.Package, error) {
	ipkg, ok := buildInfo.imports[path]
	if !ok {
		return nil, fmt.Errorf("could not find imported package %q", path)
	}
	if ipkg.pkg != nil {
		return ipkg.pkg, nil // cached
	}
	dir := os.Getenv("GARBLE_DIR")
	if dir == "" {
		return nil, fmt.Errorf("$GARBLE_DIR unset; did you run via 'garble build'?")
	}
	pkg, err := garbledImporter.ImportFrom(path, dir, 0)
	if err != nil {
		return nil, err
	}
	ipkg.pkg = pkg // cache for later use
	return pkg, nil
}

type packageInfo struct {
	buildID string
	imports map[string]importedPkg
}

type importedPkg struct {
	packagefile string
	buildID     string

	pkg *types.Package
}

func main1() int {

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Must be run with 'build' or 'ungarble' subcommand")
		return 2
	}

	var fSet customFlagSet

	switch os.Args[1] {
	case "ungarble":
		fSet = newUngarbleFlagSet()
		err := fSet.parse()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fSet.fSet().Usage()
			return 2
		}

	case "build":
		fSet = newBuildFlagSet()
		// also sets flags in environment
		err := fSet.parse()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fSet.fSet().Usage()
			return 2
		}
	}

	if err := mainErr(fSet); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func mainErr(flagSet customFlagSet) error {

	// If we recognise an argument, we're not running within -toolexec.
	switch cmd := os.Args[1]; cmd {
	case "ungarble":

		ungarbleFSet := flagSet.(*ungarbleFlagSet)

		if *ungarbleFSet.logPath == "" || *ungarbleFSet.sourcePath == "" || *ungarbleFSet.salt == "" {
			return errors.New("Missing required arguments. 'log-file', 'source-path', and 'salt' are all required")
		}

		err := ungarble.Ungarble(*ungarbleFSet.logPath, *ungarbleFSet.sourcePath, *ungarbleFSet.salt, *ungarbleFSet.outputPath)
		if err != nil {
			return err
		}

		return nil

	case "build", "test":

		buildFSet := flagSet.(*buildFlagSet)

		if len(os.Args) < 3 {
			buildFSet.flagSet.Usage()
			return errors.New("You must supply a path to the code to be garbled")
		}

		// generate salt for hashing, set as env var, and write to file
		if err := setSalt(); err != nil {
			log.Println(err)
			return err
		}

		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		os.Setenv("GARBLE_DIR", wd)
		execPath, err := os.Executable()
		if err != nil {
			return err
		}
		goArgs := []string{
			cmd,
			"-a",
			"-trimpath",
			"-toolexec=" + execPath,
		}
		if cmd == "test" {
			// vet is generally not useful on garbled code; keep it
			// disabled by default.
			goArgs = append(goArgs, "-vet=off")
		}

		userSuppliedGoFlags := strings.Split(strings.Trim(*buildFSet.goBuildFlags, " "), " ")

		goArgs = append(goArgs, userSuppliedGoFlags...)

		cmd := exec.Command("go", goArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	flag.Parse()
	if !filepath.IsAbs(flag.Args()[0]) {
		// -toolexec gives us an absolute path to the tool binary to
		// run, so this is most likely misuse of garble by a user.
		return fmt.Errorf("unknown command: %q", flag.Args()[0])
	}

	return runTransformations()
}

// app has been called by toolexec binary, now we are applying the code transformations
func runTransformations() error {
	_, tool := filepath.Split(flag.Args()[0])
	if runtime.GOOS == "windows" {
		tool = strings.TrimSuffix(tool, ".exe")
	}
	transform, ok := transformFuncs[tool]
	if !ok {
		return fmt.Errorf("unknown tool: %q", tool)
	}
	transformed := flag.Args()[1:]
	//log.Println(tool, transformed)
	if transform != nil {
		var err error
		if transformed, err = transform(transformed); err != nil {
			return err
		}
	}
	defer func() {
		for _, fn := range deferred {
			if err := fn(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}()
	cmd := exec.Command(flag.Args()[0], transformed...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// Since this program is restarted for compilation of each file,
// and we need to use the same salt, we will set the salt as
// and env variable.
func getSalt() string {
	return os.Getenv("SALT")
}

func setSalt() error {
	salt := os.Getenv("SALT")
	if salt != "" {
		// salt has already been set
		return nil
	}

	salt = uniuri.NewLen(50)
	err := os.Setenv("SALT", salt)

	// write salt to file
	log.Println("Writing salt to file. The salt is needed to ungarble stacktraces in log files.")
	err = ioutil.WriteFile("salt.txt", []byte(salt), 0644)
	if err != nil {
		return err
	}

	return  err
}

var transformFuncs = map[string]func([]string) ([]string, error){
	"compile": transformCompile,
	"link":    transformLink,

	"addr2line": nil,
	"api":       nil,
	"asm":       nil,
	"buildid":   nil,
	"cgo":       nil,
	"cover":     nil,
	"dist":      nil,
	"doc":       nil,
	"fix":       nil,
	"nm":        nil,
	"objdump":   nil,
	"pack":      nil,
	"pprof":     nil,
	"test2json": nil,
	"trace":     nil,
	"vet":       nil,
}

func transformCompile(args []string) ([]string, error) {

	flags, paths := splitFlagsFromFiles(args, ".go")
	if len(paths) == 0 {
		// Nothing to transform; probably just ["-V=full"].
		return args, nil
	}
	for i, path := range paths {
		if filepath.Base(path) == "_gomod_.go" {
			// never include module info
			paths = append(paths[:i], paths[i+1:]...)
			break
		}
	}
	if len(paths) == 1 && filepath.Base(paths[0]) == "_testmain.go" {
		return args, nil
	}

	// filters based on 'only', and 'exclude' command line flags
	if !shouldGarble(args) {
		return args, nil
	}

	// If the value of -trimpath doesn't contain the separator ';', the 'go
	// build' command is most likely not using '-trimpath'.
	trimpath := flagValue(flags, "-trimpath")
	if !strings.Contains(trimpath, ";") {
		return nil, fmt.Errorf("-toolexec=garble should be used alongside -trimpath")
	}
	if flagValue(flags, "-std") == "true" {
		return args, nil
	}
	if err := readBuildIDs(flags); err != nil {
		return nil, err
	}
	// log.Printf("%#v", ids)
	var files []*ast.File
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	info := &types.Info{
		Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object),
	}
	pkgPath := flagValue(flags, "-p")

	if _, err := origTypesConfig.Check(pkgPath, fset, files, info); err != nil {
		return nil, fmt.Errorf("typecheck error: %v", err)
	}

	outDir, err := getGarbledCodeOutputDir()
	if err != nil {
		return nil, err
	}

	// Add our out dir to the beginning of -trimpath, so that we don't
	// leak temporary dirs. Needs to be at the beginning, since there may be
	// shorter prefixes later in the list, such as $PWD if TMPDIR=$PWD/tmp.
	flags = flagSetValue(flags, "-trimpath", outDir+"=>;"+trimpath)
	// log.Println(flags)
	args = flags

	// package name is already in stdout buffer I believe. Because if we print out anything at all
	// while toolexec is running, the package name is printed out
	log.Print("")

	// TODO: randomize the order of the files
	for i, file := range files {
		origName := filepath.Base(filepath.Clean(paths[i]))

		newName := hashing.HashFileName(getSalt(), origName, file)

		name := fmt.Sprintf("%s.go", newName)
		
		switch {
		case strings.HasPrefix(origName, "_cgo_"):
			// Cgo generated code requires a prefix. Also, don't
			// garble it, since it's just generated code and it gets
			// messy.
			name = "_cgo_" + name
		default:
			//fmt.Printf("transforming %s\n", name)
			file = transformGo(file, info)
		}
		tempFile := filepath.Join(outDir, name)
		f, err := os.Create(tempFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		// printerConfig.Fprint(os.Stderr, fset, file)
		if err := printerConfig.Fprint(f, fset, file); err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		args = append(args, f.Name())
	}

	// obfuscate strings
	if os.Getenv("SKIP_STRINGS") != "TRUE" {
		err = stringsG.ObfuscateStrings(outDir)
		if err != nil {
			log.Println(err)
			return nil, err
		}
	}


	return args, nil
}

// filters based on 'only' and 'exclude' command line flags
func shouldGarble(args []string) bool {
	currentPackageName := pkgNameFromBuildArgs(args)

	onlyThisPkgName := os.Getenv("ONLY")
	if onlyThisPkgName != "" && !strings.HasPrefix(currentPackageName, onlyThisPkgName) {
		return false
	}

	excludeEnvVal := os.Getenv("EXCLUDE")
	if excludeEnvVal != "" {
		exclude := strings.Split(os.Getenv("EXCLUDE"), ",")
		for _, pkgName := range exclude {
			if strings.HasPrefix(currentPackageName, pkgName) {
				return false
			}
		}
	}

	return true
}

func pkgNameFromBuildArgs(args []string) string {
	pathArg := args[3]
	pathArgSplit := strings.Split(pathArg, "=>")
	packageName := pathArgSplit[len(pathArgSplit)-1]
	return packageName
}

// Either return the directory specified by the user, or
// create a temp directory
func getGarbledCodeOutputDir() (string, error) {

	outputDir := os.Getenv("CODE_OUT_DIR")

	// The real reason we create a new directory here,
	// is because if garble build was run twice using the same output directory
	// for source files, it would tend to hang, using lots of cpu like it was stuck
	// in a loop. Not sure why, so might as well just create a new dir each time,
	// and might as well use the salt as the name.
	if outputDir != "" {
		salt := os.Getenv("SALT")

		finalOutputDir := filepath.Join(outputDir, salt)

		err := os.MkdirAll(finalOutputDir, os.ModePerm)
		if err != nil {
			return "", err
		}
		return finalOutputDir, nil
	}

	// since they didn't pass a dir, we use a temp dir
	tempDir, err := ioutil.TempDir("", "garble-build-")
	if err != nil {
		return "", err
	}

	// clean up temp dir later
	deferred = append(deferred, func() error {
		return os.RemoveAll(tempDir)
	})

	return tempDir, nil
}

func readBuildIDs(flags []string) error {
	buildInfo.buildID = flagValue(flags, "-buildid")
	switch buildInfo.buildID {
	case "", "true":
		return fmt.Errorf("could not find -buildid argument")
	}
	buildInfo.buildID = trimBuildID(buildInfo.buildID)
	importcfg := flagValue(flags, "-importcfg")
	if importcfg == "" {
		return fmt.Errorf("could not find -importcfg argument")
	}
	data, err := ioutil.ReadFile(importcfg)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, " ")
		if i < 0 {
			continue
		}
		if verb := line[:i]; verb != "packagefile" {
			continue
		}
		args := strings.TrimSpace(line[i+1:])
		j := strings.Index(args, "=")
		if j < 0 {
			continue
		}
		importPath, objectPath := args[:j], args[j+1:]
		fileID, err := buildidOf(objectPath)
		if err != nil {
			return err
		}
		buildInfo.imports[importPath] = importedPkg{
			packagefile: objectPath,
			buildID:     fileID,
		}
	}
	// log.Printf("%#v", buildInfo)
	return nil
}

func trimBuildID(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.IndexByte(id, '/'); i > 0 {
		id = id[:i]
	}
	return id
}

func buildidOf(path string) (string, error) {
	cmd := exec.Command("go", "tool", "buildid", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, out)
	}
	return trimBuildID(string(out)), nil
}

// for debugging
func reasonNotHashed(name, reason, path string) {
	fmt.Printf("Name: %s, Reason: %s Path: %s \n", name, reason, path);
}

// transformGo garbles the provided Go syntax node.
func transformGo(file *ast.File, info *types.Info) *ast.File {
	// Remove all comments, minus the "//go:" compiler directives.
	// The final binary should still not contain comment text, but removing
	// it helps ensure that (and makes position info less predictable).
	origComments := file.Comments
	file.Comments = nil
	for _, commentGroup := range origComments {
		for _, comment := range commentGroup.List {
			if strings.HasPrefix(comment.Text, "//go:") {
				file.Comments = append(file.Comments, &ast.CommentGroup{
					List: []*ast.Comment{comment},
				})
			}
		}
	}

	pre := func(cursor *astutil.Cursor) bool {
		
		switch node := cursor.Node().(type) {
			
		case *ast.Ident:
			//fmt.Println("Original node name: ", node.Name)

			if node.Name == "_" {
				return true // unnamed remains unnamed
			}

			if strings.HasPrefix(node.Name, "_C") || strings.Contains(node.Name, "_cgo") {
				return true // don't mess with cgo-generated code
			}

			obj := info.ObjectOf(node)

			// log.Printf("%#v %T", node, obj)

			switch x := obj.(type) {

			case *types.Var:
				if x.Embedded() {
					obj = objOf(obj.Type())
				} else if x.IsField() && x.Exported() {
					// might be used for reflection, e.g.
					// encoding/json without struct tags
					//reasonNotHashed(node.Name, "Might be used for reflection", "")
					return true
				}

			case *types.Const:
			case *types.TypeName:
			case *types.Func:
				sign := obj.Type().(*types.Signature)
				if obj.Exported() && sign.Recv() != nil {
					//reasonNotHashed(node.Name, "Might implement an interface", "")
					return true // might implement an interface
				}
				if implementedOutsideGo(x) {
					//reasonNotHashed(node.Name, "implemented outside go", "")
					return true // give up in this case
				}
				switch node.Name {
				case "main", "init", "TestMain":
					//reasonNotHashed(node.Name, "files we don't want to break", "")
					return true // don't break them
				}
				if strings.HasPrefix(node.Name, "Test") && isTestSignature(sign) {
					//reasonNotHashed(node.Name, "Is test", "")
					return true // don't break tests
				}

			case nil:
				switch cursor.Parent().(type) {
				case *ast.AssignStmt:
					// symbolic var v in v := expr.(type)
				default:
					//reasonNotHashed(node.Name, "hit default in nil case", "")
					return true
				}
			default:
				//reasonNotHashed(node.Name, "hit default in main case", "")
				return true // we only want to rename the above
			}
			//buildID := buildInfo.buildID
			if obj != nil {
				pkg := obj.Pkg()
				if pkg == nil {
					//reasonNotHashed(node.Name, "Universe scope", "")
					return true // universe scope
				}
				path := pkg.Path()
				if isStandardLibrary(path) {
					//reasonNotHashed(node.Name, "Is standard lib", path)
					return true // std isn't transformed
				}

				if id := buildInfo.imports[path].buildID; id != "" {
					garbledPkg, err := garbledImport(path)
					if err != nil {
						panic(err) // shouldn't happen
					}
					// Check if the imported name wasn't
					// garbled, e.g. if it's assembly.
					if garbledPkg.Scope().Lookup(obj.Name()) != nil {
						//reasonNotHashed(node.Name, "It is assembly or something", "")
						return true
					}
					//buildID = id
				}
			}
			//orig := node.Name

			node.Name = hashing.HashWith(getSalt(), node.Name)
			// node.Name = hashing.HashWith(buildID, node.Name)

			//log.Printf("%q hashed with %q to %q", orig, buildID, node.Name)
		}
		
		return true
	}
	return astutil.Apply(file, pre, nil).(*ast.File)
}

func isStandardLibrary(path string) bool {

	if path == "main" {
		// Main packages may not have fully qualified import paths, but
		// they're not part of the standard library
		return false
	}

	// include user defined packages that don't have a fully qualified import path
	includeEnv := os.Getenv("INCLUDE")
	if includeEnv != "" { // if we don't do this check, and it's empty, everything will match
		include := strings.Split(includeEnv, ",")
		for _, pkgName := range include {
			if strings.HasPrefix(path, pkgName) {
				fmt.Println("Has prefix")
				return false
			}
		}
	}

	onlyEnv := os.Getenv("ONLY")
	if onlyEnv != "" {
		return !strings.HasPrefix(path, onlyEnv)
	}


	return !strings.Contains(path, ".")
}

// implementedOutsideGo returns whether a *types.Func does not have a body, for
// example when it's implemented in assembly, or when one uses go:linkname.
//
// Note that this function can only return true if the obj parameter was
// type-checked from source - that is, if it's the top-level package we're
// building. Dependency packages, whose type information comes from export data,
// do not differentiate these "external funcs" in any way.
func implementedOutsideGo(obj *types.Func) bool {
	return obj.Type().(*types.Signature).Recv() == nil &&
		(obj.Scope() != nil && obj.Scope().Pos() == token.NoPos)
}

func objOf(t types.Type) types.Object {
	switch t := t.(type) {
	case *types.Named:
		return t.Obj()
	case interface{ Elem() types.Type }:
		return objOf(t.Elem())
	default:
		return nil
	}
}

// isTestSignature returns true if the signature matches "func _(*testing.T)".
func isTestSignature(sign *types.Signature) bool {
	if sign.Recv() != nil {
		return false
	}
	params := sign.Params()
	if params.Len() != 1 {
		return false
	}
	obj := objOf(params.At(0).Type())
	return obj != nil && obj.Pkg().Path() == "testing" && obj.Name() == "T"
}

func transformLink(args []string) ([]string, error) {

	flags, paths := splitFlagsFromFiles(args, ".a")
	if len(paths) == 0 {
		// Nothing to transform; probably just ["-V=full"].
		return args, nil
	}
	flags = append(flags, "-w", "-s")

	return append(flags, paths...), nil
}

// splitFlagsFromFiles splits args into a list of flag and file arguments. Since
// we can't rely on "--" being present, and we don't parse all flags upfront, we
// rely on finding the first argument that doesn't begin with "-" and that has
// the extension we expect for the list of paths.
func splitFlagsFromFiles(args []string, ext string) (flags, paths []string) {
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") && strings.HasSuffix(arg, ext) {
			return args[:i:i], args[i:]
		}
	}
	return args, nil
}

// booleanFlag records which of the flags that we need are boolean. This
// matters, because boolean flags never consume the following argument, while
// non-boolean flags always do.
//
// For now, this stati
func booleanFlag(name string) bool {
	switch name {
	case "-std":
		return true
	default:
		return false
	}
}

// flagValue retrieves the value of a flag such as "-foo", from strings in the
// list of arguments like "-foo=bar" or "-foo" "bar".
func flagValue(flags []string, name string) string {
	isBool := booleanFlag(name)
	for i, arg := range flags {
		if val := strings.TrimPrefix(arg, name+"="); val != arg {
			// -name=value
			return val
		}
		if arg == name { // -name ...
			if isBool {
				// -name, equivalent to -name=true
				return "true"
			}
			if i+1 < len(flags) {
				// -name value
				return flags[i+1]
			}
		}
	}
	return ""
}

func flagSetValue(flags []string, name, value string) []string {
	isBool := booleanFlag(name)
	for i, arg := range flags {
		if strings.HasPrefix(arg, name+"=") {
			// -name=value
			flags[i] = name + "=" + value
			return flags
		}
		if arg == name { // -name ...
			if isBool {
				// -name, equivalent to -name=true
				flags[i] = name + "=" + value
				return flags
			}
			if i+1 < len(flags) {
				// -name value
				flags[i+1] = value
				return flags
			}
			return flags
		}
	}
	return append(flags, name+"="+value)
}
