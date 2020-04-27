# garble

This is a fork of https://github.com/mvdan/garble with several improvements.

Improvements over original:

- Ungarble stack traces in log files, outputting a new logfile
- String obfuscation by default
- Flags to include, exclude, or only garble certain packages
- Flag to specify output path for garbled code for inspection

You can run 'garble build' or 'garble ungarble' for complete usage and list of available flags

Installation:

	GO111MODULE=on git clone github.com/digitalhurricane-io/garble && cd garble && go install

Obfuscate a Go build. Requires Go 1.13 or later.

	garble build [package] [flags]

Ungarble stack traces in a log file, outputting a new file:

    garble ungarble --log-path ./logA.txt --source-path ./ --salt z0hDIP5lGMVlCMQUn3F4Wno70yPdDdJi32Hvj6Q9OB6Tu08LNp

While the original garble project made efforts to support gopath, this fork has not given any thought to supporting gopath.
Maybe everything still works with gopath. But I only use it with modules.

## Original Readme Text Below
### Purpose

Produce a binary that works as well as a regular build, but that has as little
information about the original source code as possible.

The tool is designed to be:

* Coupled with `cmd/go`, to support both `GOPATH` and modules with ease
* Deterministic and reproducible, given the same initial source code
* Reversible given the original source, to un-garble panic stack traces

### Mechanism

The tool wraps calls to the Go compiler to transform the Go source code, in
order to:

* Replace as many useful identifiers as possible with short base64 hashes
* Remove [module build information](https://golang.org/pkg/runtime/debug/#ReadBuildInfo)
* Strip filenames and unnecessary lines, to make position info less useful

It also wraps calls to the linker in order to:

* Enforce the `-s` flag, to not include the symbol table
* Enforce the `-w` flag, to not include DWARF debugging data

Finally, the tool requires the use of the `-trimpath` build flag, to ensure the
binary doesn't include paths from the current filesystem.

### Caveats

Most of these can improve with time and effort. The purpose of this section is
to document the current shortcomings of this tool.

* Package import path names are never garbled, since we require the original
  paths for the build system to work. See #13 to investigate alternatives.

* The `-a` flag for `go build` is required, since `-toolexec` doesn't work well
  with the build cache; see [golang/go#27628](https://github.com/golang/go/issues/27628).

* Since no caching at all can take place right now (see the link above), fast
  incremental builds aren't possible. Large projects might be slow to build.

* The standard library is never garbled when compiled, since the source is
  always publicly available. See #7 for making this configurable.

* Deciding what method names to garble is always going to be difficult, due to
  interfaces that could be implemented up or down the package import tree. At
  the moment, exported methods are never garbled.

* Similarly to methods, exported struct fields are difficult to garble, as the
  names might be relevant for reflection work like `encoding/json`. At the
  moment, exported methods are never garbled.

* Functions implemented outside Go, such as assembly, aren't garbled since we
  currently only transform the input Go source.
