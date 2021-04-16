package mocks

import (
	"github.com/stretchr/testify/mock"
	"io/fs"
	"time"
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

func (f *FileInfoMock) Sys() interface{} {
	args := f.Called()
	return args.Get(0)
}
