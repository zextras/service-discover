package test

import (
	"io/ioutil"
	"os"
)

func GenerateRandomFile(testName string) *os.File {
	file, err := ioutil.TempFile("/tmp", testName)
	if err != nil {
		panic(err)
	}
	return file
}

func GenerateRandomFolder(prefix string) string {
	file, err := ioutil.TempDir("/tmp", prefix)
	if err != nil {
		panic(err)
	}
	return file
}
