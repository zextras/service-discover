package credentialsEncrypter

import (
	"archive/tar"
	"bitbucket.org/zextras/service-discover/cli/lib/test"
	"bufio"
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
	"time"
)

type fileStatStub struct {
	fileName string
	data     *bytes.Buffer
}

func (f *fileStatStub) Name() string {
	return f.fileName
}

func (f *fileStatStub) Size() int64 {
	return int64(f.data.Len())
}

func (f *fileStatStub) Mode() os.FileMode {
	return os.FileMode(0755)
}

func (f *fileStatStub) ModTime() time.Time {
	return time.Now()
}

func (f *fileStatStub) IsDir() bool {
	return false
}

func (f *fileStatStub) Sys() interface{} {
	panic("stub!")
}

func TestWriter(t *testing.T) {
	t.Parallel()

	type outputsetup struct {
		encOutput bytes.Buffer
		file1     bytes.Buffer
		file2     bytes.Buffer
	}
	setup := func() (*outputsetup, func()) {
		encOutput := bytes.Buffer{}
		file1 := bytes.NewBuffer([]byte("test1"))
		file2 := bytes.NewBuffer([]byte("test2"))

		return &outputsetup{
				encOutput: encOutput,
				file1:     *file1,
				file2:     *file2,
			}, func() {
			}
	}

	type fields struct {
	}
	type args struct {
		reader   io.Reader
		password string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name:   "Add files with relative path reader",
			fields: fields{},
			args: args{
				// the reader will be set afterwards
				password: "password",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testData, cleanup := setup()
			defer cleanup()
			writer := createWriterForEncryption(t, &testData.encOutput, tt.args.password)
			fileStub1 := &fileStatStub{fileName: "file1", data: &testData.file1}
			fileStub2 := &fileStatStub{fileName: "file2", data: &testData.file2}
			assert.NoError(
				t,
				writer.AddFile(bufio.NewReader(&testData.file1), fileStub1, "", "/"),
				"file1 insertion should complete without error",
			)
			assert.NoError(
				t,
				writer.AddFile(bufio.NewReader(&testData.file2), fileStub2, "", "/"),
				"file2 insertion should complete without error",
			)
			assert.NoError(t, writer.Close())
			pipeReader, pipeWriter, err := os.Pipe()
			assert.NoError(t, err)
			defer pipeReader.Close()
			defer pipeWriter.Close()

			stdout := bytes.Buffer{}
			stderr := bytes.Buffer{}
			encTar, err := ioutil.ReadAll(&testData.encOutput)
			assert.NoError(t, err)
			gpgCliCmd := exec.Command(
				"gpg",
				"--batch",
				"--yes",
				"--quiet",
				"--passphrase",
				tt.args.password,
				"--decrypt",
				"-",
			)
			gpgCliCmd.Stdin = pipeReader
			gpgCliCmd.Stdout = &stdout
			gpgCliCmd.Stderr = &stderr
			assert.NoError(t, gpgCliCmd.Start())
			_, err = pipeWriter.Write(encTar)
			assert.NoError(t, pipeWriter.Close())
			assert.NoError(t, err)
			assert.NoError(t, gpgCliCmd.Wait(), fmt.Sprintf("stderr output: %s", stderr.String()))
			resultReader := tar.NewReader(&stdout)
			listOfCompressedFiles := make([]*fileStatStub, 0)
			for {
				header, err := resultReader.Next()
				if err == io.EOF {
					t.Log("Reached EOF")
					break
				}
				assert.NoError(t, err, "Error while reading tar file")
				t.Logf("Header name file: %s\n", header.Name)
				bs, err := ioutil.ReadAll(resultReader)
				t.Logf("Content of %s: %s", header.Name, string(bs))
				assert.NoError(t, err)
				listOfCompressedFiles = append(listOfCompressedFiles, &fileStatStub{
					fileName: header.Name,
					data:     bytes.NewBuffer(bs),
				})
			}

			// Note: we use relative path since the tarball will not have absolute paths.
			expectedFileList := make([]*fileStatStub, 0)
			expectedFileList = append(expectedFileList, fileStub1)
			expectedFileList = append(expectedFileList, fileStub2)
			assert.Equal(
				t,
				len(expectedFileList),
				len(listOfCompressedFiles),
				"The number of elements in the array is not the wanted one",
			)
		})
	}
}

func createWriterForEncryption(t *testing.T, buf *bytes.Buffer, pass string) *Writer {
	writer, err := NewWriter(buf, []byte(pass))
	assert.NoError(t, err)
	return writer
}

func ExampleWriter_AddFile() {
	// The resulting encrypted tarball
	encTar := bytes.Buffer{}

	// Testing utility that generates and unique random file for testing purposes
	fileToAdd := test.GenerateRandomFile("ExampleWriter_AddFile")
	if err := ioutil.WriteFile(fileToAdd.Name(), []byte("Hello world"), os.FileMode(0755)); err != nil {
		panic(err)
	}

	// Let's create a new encrypted writer that will encrypt the tarball with the password "password"
	writer, err := NewWriter(&encTar, []byte("password"))
	if err != nil {
		panic(err)
	}
	fileToAddStat, err := fileToAdd.Stat()
	if err != nil {
		panic(nil)
	}

	// This will add the file in the root of the tarball
	if err := writer.AddFile(fileToAdd, fileToAddStat, "", "/"); err != nil {
		panic(err)
	}
	// This will add the file in "/test/", but the path will be always relative, i.e. "test/"
	if err := writer.AddFile(fileToAdd, fileToAddStat, "", "/test"); err != nil {
		panic(err)
	}
	// Passing a relative path will always be interpreted as starting from the root of the tarball. This is the same of
	// writing "/dist"
	if err := writer.AddFile(fileToAdd, fileToAddStat, "", "dist"); err != nil {
		panic(err)
	}
}
