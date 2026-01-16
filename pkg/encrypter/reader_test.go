// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package encrypter

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	mocks2 "github.com/zextras/service-discover/test/mocks"
)

// createEncryptedArchive creates an encrypted archive using NewWriter with the given password.
func createEncryptedArchive(t *testing.T, password string) (*bytes.Buffer, []byte) {
	t.Helper()

	encFile := &bytes.Buffer{}
	encWriter, err := NewWriter(encFile, []byte(password))
	assert.NoError(t, err)

	dummyFile := []byte("TestString")
	dummyFileStat := new(mocks2.FileInfoMock)
	dummyFileStat.
		On("Name").Return("test").
		On("Size").Return(int64(len(dummyFile))).
		On("Mode").Return(os.FileMode(0755)).
		On("ModTime").Return(time.Now())

	assert.NoError(t, encWriter.AddFile(
		bytes.NewBuffer(dummyFile),
		dummyFileStat,
		"test",
		"/",
	))
	assert.NoError(t, encWriter.Close())

	return encFile, dummyFile
}

func TestNewReader(t *testing.T) {
	t.Parallel()

	t.Run("Open a valid encrypted archive with valid password", func(t *testing.T) {
		encFile, dummyFile := createEncryptedArchive(t, "password")

		encReader, err := NewReader(encFile, []byte("password"))
		assert.NoError(t, err)

		type tarFile struct {
			name string
			data []byte
		}

		listOfCompressedFiles := make([]tarFile, 0)

		for {
			header, err := encReader.Next()
			if err == io.EOF {
				t.Log("Reached EOF")

				break
			}

			assert.NoError(t, err, "Error while reading tar file")
			t.Logf("Header name file: %s\n", header.Name)

			bs, _ := io.ReadAll(encReader)
			listOfCompressedFiles = append(listOfCompressedFiles, tarFile{
				name: header.Name,
				data: bs,
			})
		}

		assert.Equal(
			t,
			1,
			len(listOfCompressedFiles),
			"Only one file should be contained in this archive",
		)

		assert.Equal(t, dummyFile, listOfCompressedFiles[0].data)
	})

	t.Run("Gives error when an archive with wrong password is accessed", func(t *testing.T) {
		encFile, _ := createEncryptedArchive(t, "password")

		_, err := NewReader(encFile, []byte("wrong_password"))
		assert.Error(t, err)
	})
}

func TestReadFiles(t *testing.T) {
	t.Parallel()
	t.Run("Extract all desired files", func(t *testing.T) {
		clearTar := bytes.Buffer{}
		encWriter, err := NewWriter(&clearTar, []byte("password"))
		assert.NoError(t, err)

		defer encWriter.Close()

		dummyFile := []byte("TestString")
		dummyFileStat := new(mocks2.FileInfoMock)
		dummyFileStat.
			On("Name").
			Return("test").
			On("Size").
			Return(int64(len(dummyFile))).
			On("Mode").
			Return(os.FileMode(0644)).
			On("ModTime").
			Return(time.Now())
		assert.NoError(t, encWriter.AddFile(
			bytes.NewBuffer(dummyFile),
			dummyFileStat,
			"test",
			"/",
		))

		dummyFile2 := []byte("TestString2")
		dummyFileStat2 := new(mocks2.FileInfoMock)
		dummyFileStat2.
			On("Name").
			Return("test").
			On("Size").
			Return(int64(len(dummyFile2))).
			On("Mode").
			Return(os.FileMode(0644)).
			On("ModTime").
			Return(time.Now())
		assert.NoError(t, encWriter.AddFile(
			bytes.NewBuffer(dummyFile2),
			dummyFileStat2,
			"test2",
			"/",
		))
		assert.NoError(t, encWriter.Close())

		reader, err := NewReader(&clearTar, []byte("password"))
		assert.NoError(t, err)

		actualFiles, err := ReadFiles(reader, "test", "test2")
		assert.NoError(t, err)
		assert.Len(t, actualFiles, 2, "Expected two files extracted")

		assert.Equal(t, dummyFile, actualFiles["test"])
		assert.Equal(t, dummyFile2, actualFiles["test2"])
	})

	t.Run("Returns error when asking non-existing content", func(t *testing.T) {
		clearTar := bytes.Buffer{}
		encWriter, err := NewWriter(&clearTar, []byte("password"))
		assert.NoError(t, err)

		defer encWriter.Close()

		dummyFile := []byte("TestString")
		dummyFileStat := new(mocks2.FileInfoMock)
		dummyFileStat.
			On("Name").
			Return("test").
			On("Size").
			Return(int64(len(dummyFile))).
			On("Mode").
			Return(os.FileMode(0644)).
			On("ModTime").
			Return(time.Now())
		assert.NoError(t, encWriter.AddFile(
			bytes.NewBuffer(dummyFile),
			dummyFileStat,
			"test",
			"/",
		))

		dummyFile2 := []byte("TestString2")
		dummyFileStat2 := new(mocks2.FileInfoMock)
		dummyFileStat2.
			On("Name").
			Return("test").
			On("Size").
			Return(int64(len(dummyFile2))).
			On("Mode").
			Return(os.FileMode(0644)).
			On("ModTime").
			Return(time.Now())
		assert.NoError(t, encWriter.AddFile(
			bytes.NewBuffer(dummyFile2),
			dummyFileStat2,
			"test2",
			"/",
		))
		assert.NoError(t, encWriter.Close())

		reader, err := NewReader(&clearTar, []byte("password"))
		assert.NoError(t, err)

		_, err = ReadFiles(reader, "test", "test2", "iDontExists")
		assert.EqualError(t, err, "not all files where found in the archive: iDontExists")
	})
}
