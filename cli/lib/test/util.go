// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package test

import (
	"bytes"
	"os"
	"time"

	"github.com/Zextras/service-discover/cli/lib/test/mocks"
)

func GenerateRandomFile(testName string) *os.File {
	file, err := os.CreateTemp("/tmp", testName)
	if err != nil {
		panic(err)
	}
	return file
}

func GenerateRandomFolder(prefix string) string {
	file, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		panic(err)
	}
	return file
}

func CreateDumbFile(content []byte, name string) (*bytes.Buffer, *mocks.FileInfoMock) {
	dumbContent := bytes.NewBuffer(content)
	caStat := new(mocks.FileInfoMock)
	caStat.On("Name").
		Return(name).
		On("Size").
		Return(int64(dumbContent.Len())).
		On("Mode").
		Return(os.FileMode(0644)).
		On("ModTime").
		Return(time.Now())
	return dumbContent, caStat
}
