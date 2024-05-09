// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package encrypter

import (
	"archive/tar"
	"bytes"
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgpErrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/pkg/errors"
)

// NewReader returns a pointer to a tar.Reader. This reader automatically decrypts the passed reader, assuming it is a
// tarball symmetrically encrypted with OpenPGP.
func NewReader(reader io.Reader, passphrase []byte) (*tar.Reader, error) {
	decoder, err := armor.Decode(reader)
	if err != nil {
		return nil, err
	}

	firstTime := true
	passGenerator := func(_ []openpgp.Key, _ bool) ([]byte, error) {
		if firstTime {
			firstTime = false

			return passphrase, nil
		}

		return nil, pgpErrors.ErrKeyIncorrect
	}

	message, err := openpgp.ReadMessage(decoder.Body, nil, passGenerator, nil)
	if err != nil {
		return nil, err
	}

	return tar.NewReader(message.UnverifiedBody), nil
}

// ReadFile reads the content of the currently cursor in the tar reader and returns it as an array of bytes. Note, you
// still have to call tarReader.Next() in order to iterate over all the tarball files.
func ReadFile(tarReader *tar.Reader) ([]byte, error) {
	contentBuffer := &bytes.Buffer{}
	if _, err := io.Copy(contentBuffer, tarReader); err != nil {
		return nil, err
	}

	bs, err := io.ReadAll(contentBuffer)
	if err != nil {
		return nil, err
	}

	return bs, nil
}

// ReadFiles reads multiples files. It iterates over the tarReader, calling Next() for you. All the files passed must
// exists, otherwise an error will be returned. The path passed in files must be equal to the one contained inside the
// tarball, otherwise the file will not be found.
func ReadFiles(tarReader *tar.Reader, files ...string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	remainingFiles := files

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}

		for index, fileName := range files {
			if header.Name == fileName {
				content, err := ReadFile(tarReader)
				if err != nil {
					return nil, err
				}

				result[fileName] = content
				remainingFiles[index] = remainingFiles[len(remainingFiles)-1]
				remainingFiles = remainingFiles[:len(remainingFiles)-1]

				break
			}
		}
	}

	if len(result) != len(files) {
		missingFiles := ""
		for _, f := range remainingFiles {
			missingFiles += " " + f
		}

		return nil, errors.Errorf("not all files where found in the archive:%s", missingFiles)
	}

	return result, nil
}
