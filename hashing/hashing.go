package hashing


import (
	"crypto/sha256"
	"encoding/base64"
	"go/ast"
	"io"
	"go/token"
	"log"
)

var b64 = base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_z")

// The hashed filename is a combination of the package name and file name
// eg sha256(pkgName + filename)
func HashFileName(salt string, originalName string, file *ast.File) string {
	pkgName := getPackageName(file)
	log.Println("package name: ",pkgName)
	combined := pkgName + originalName
	return HashWith(salt, combined)
}

func getPackageName(node ast.Node) string {
    switch x := node.(type) {
    case *ast.File:
        return x.Name.String()
    }
    return ""
}

func HashWith(salt, value string) string {
	const length = 8

	d := sha256.New()
	io.WriteString(d, salt)
	io.WriteString(d, value)
	sum := b64.EncodeToString(d.Sum(nil))

	if token.IsExported(value) {
		return "Z" + sum[:length]
	}
	return "z" + sum[:length]
}