package ungarble

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)




// origLookup helps implement a types.Importer which finds the export data for
// the original dependencies, not their garbled counterparts. This is useful to
// typecheck a package before it's garbled, so we can make decisions on how to
// garble it.
func origLookup(path string) (io.ReadCloser, error) {
	cmd := exec.Command("go", "list", "-json", "-export", path)
	//wd, _ := os.Getwd()
	//if err != nil {
		//return err
	//}
	cmd.Dir = originalSourcePath
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

// the pkgPath param is the import like you would use at the top of the file to import the package
func getTypesInfo(filePath string, file *ast.File) (*types.Info, error) {

	info := &types.Info{
		Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object),
	}

	files := []*ast.File{file}

	pkgImportPath, err := getPkgImportPathFromFilePath(filePath)
	if err != nil {
		return nil, err
	}

	if _, err := origTypesConfig.Check(pkgImportPath, fileSet, files, info); err != nil {
		return nil, fmt.Errorf("typecheck error: %v", err)
	}

	return info, nil
}

// Given a file path, gets the import path for package as you would use in an import at the top of a file.
func getPkgImportPathFromFilePath(filePath string) (string, error) {
	dir := filepath.Dir(filePath)
	cmd := exec.Command("go", "list", "-json", "-export", dir)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go list error: %v: %s", err, out)
	}
	var res struct {
		ImportPath string
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return "", err
	}
	return res.ImportPath, nil
}