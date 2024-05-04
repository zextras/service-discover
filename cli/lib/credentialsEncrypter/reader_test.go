// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package credentialsEncrypter

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
	"time"

	mocks2 "github.com/Zextras/service-discover/cli/lib/test/mocks"
	"github.com/stretchr/testify/assert"
)

func TestNewReader(t *testing.T) {
	t.Parallel()
	type args struct {
		reader     io.Reader
		passphrase []byte
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Open a valid gpg armored archived with valid password",
			args: args{
				reader:     nil,
				passphrase: []byte("password"),
			},
			wantErr: false,
		},
		{
			name: "Gives error when an archive with wrong password is accessed",
			args: args{
				reader:     nil,
				passphrase: []byte("wrong_password"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTar := bytes.Buffer{}
			tarWriter := tar.NewWriter(&clearTar)
			defer tarWriter.Close()
			dummyFile := []byte("TestString")
			assert.NoError(t, tarWriter.WriteHeader(&tar.Header{
				Name:    "test",
				Size:    int64(len(dummyFile)),
				Mode:    int64(os.FileMode(0755)),
				ModTime: time.Now(),
			}))
			_, err := io.Copy(tarWriter, bytes.NewBuffer(dummyFile))
			assert.NoError(t, err)
			assert.NoError(t, tarWriter.Close())

			pipeReader, pipeWriter, err := os.Pipe()
			assert.NoError(t, err)
			defer pipeReader.Close()
			defer pipeWriter.Close()
			encFile := bytes.Buffer{}
			stderr := bytes.Buffer{}
			// gpg --symmetric --cipher-algo aes256
			gpgEncCmd := exec.Command(
				"gpg",
				"--batch",
				"--yes",
				"--armor",
				"--quiet",
				"--passphrase",
				"password",
				"--symmetric",
				"--cipher-algo",
				"aes256",
				"--output",
				"-",
			)
			gpgEncCmd.Stdin = pipeReader
			gpgEncCmd.Stdout = &encFile
			gpgEncCmd.Stderr = &stderr
			bs, err := ioutil.ReadAll(&clearTar)
			assert.NoError(t, err)
			_, err = pipeWriter.Write(bs)
			assert.NoError(t, err)
			assert.NoError(t, pipeWriter.Close())
			assert.NoError(t, gpgEncCmd.Run(), stderr.String())
			assert.NoError(t, pipeReader.Close())

			encReader, err := NewReader(&encFile, tt.args.passphrase)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
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
					bs, err := ioutil.ReadAll(encReader)
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

				// Let's read the content and see if it is equal to the one created in the setup
				assert.NoError(t, err)
				assert.Equal(t, dummyFile, listOfCompressedFiles[0].data)
			}
		})
	}
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
