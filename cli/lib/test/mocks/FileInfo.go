/*
 * Copyright (C) 2023 Zextras srl
 *
 *     This program is free software: you can redistribute it and/or modify
 *     it under the terms of the GNU Affero General Public License as published by
 *     the Free Software Foundation, either version 3 of the License, or
 *     (at your option) any later version.
 *
 *     This program is distributed in the hope that it will be useful,
 *     but WITHOUT ANY WARRANTY; without even the implied warranty of
 *     MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *     GNU Affero General Public License for more details.
 *
 *     You should have received a copy of the GNU Affero General Public License
 *     along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 */

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
