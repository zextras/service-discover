// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package mocks

import (
	"io/fs"
	"time"

	"github.com/stretchr/testify/mock"
)

type FileInfoMock struct {
	mock.Mock
}

func (f *FileInfoMock) Name() string {
	args := f.Called()
	return args.String(0)
}

func (f *FileInfoMock) Size() int64 {
	args := f.Called()
	return args.Get(0).(int64)
}

func (f *FileInfoMock) Mode() fs.FileMode {
	args := f.Called()
	return args.Get(0).(fs.FileMode)
}

func (f *FileInfoMock) ModTime() time.Time {
	args := f.Called()
	return args.Get(0).(time.Time)
}

func (f *FileInfoMock) IsDir() bool {
	args := f.Called()
	return args.Bool(0)
}

func (f *FileInfoMock) Sys() any {
	args := f.Called()
	return args.Get(0)
}
