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

package test

import (
	"bytes"
	"github.com/Zextras/service-discover/cli/lib/test/mocks"
	"os"
	"time"
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
