package main

import (
	"path/filepath"
	"math/rand"
)

func isGoFile(path string) bool {
	return filepath.Ext(path) == ".go"
}

func randomString(length int) string {

	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

	b := make([]rune, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}