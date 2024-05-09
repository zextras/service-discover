// SPDX-FileCopyrightText: 2022-2024 Zextras <https://www.zextras.com>
//
// SPDX-License-Identifier: AGPL-3.0-only

package encrypter

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/pkg/errors"
)

// Writer represents an encryption reader. The encryption is performed with OpenPGP, and it is performed in a symmetric
// way. The generated file is a tarball one, _without_ any compression.
type Writer struct {
	armorWriter   io.WriteCloser
	openPgpWriter io.WriteCloser
	tarballWriter *tar.Writer
}

func (e *Writer) Write(p []byte) (n int, err error) {
	return e.tarballWriter.Write(p)
}

func (e *Writer) Close() error {
	if err := e.tarballWriter.Close(); err != nil {
		return err
	}

	if err := e.openPgpWriter.Close(); err != nil {
		return err
	}

	if err := e.armorWriter.Close(); err != nil {
		return err
	}

	return nil
}

// Flush method allows to flush the underlying tarball reader into the destination.
func (e *Writer) Flush() error {
	return e.tarballWriter.Flush()
}

// AddFile allows to simply add a file to the current encrypted tarball. You can customize the destination path where
// the file will be inserted in the archive. Insertion of a file in the same path on the tarball of another one already
// existing will result in an undefined behaviour. The path can be absolute, and in this case it will refer to the root
// of the tarball. Please note that all the resulting paths in the archive will be relative though, in order to avoid
// problems with extraction programs (e.g. Ark), so writing "/" and "" as third parameter will have the same result.
func (e *Writer) AddFile(reader io.Reader, stat os.FileInfo, customFilename string, directoryOnArchive string) error {
	filename := customFilename
	if filename == "" {
		filename = stat.Name()
	}
	// Now lets create the header as needed for this file within the tarball
	directory := filepath.Dir(directoryOnArchive)

	relativePath, err := filepath.Rel("/", directory)
	if err != nil {
		return err
	}

	headerName := filepath.Join(relativePath, filename)

	header := new(tar.Header)
	header.Name = headerName
	header.Size = stat.Size()
	header.Mode = int64(stat.Mode())
	header.ModTime = stat.ModTime()
	// Write the header to the tarball archive
	if err := e.tarballWriter.WriteHeader(header); err != nil {
		return err
	}
	// Copy the file data to the tarball
	if _, err := io.Copy(e.tarballWriter, reader); err != nil {
		return err
	}

	return nil
}

// NewWriter initialize a new reader that automatically encrypts with OpenPGP the data passed. Additionally, the data
// is wrapped around with a PGP armor, making the file text-based and easier to manipulate.
// The encryption defaults are the "sane defaults" set by the openpgp package this reader is based on. Please check
// https://pkg.go.dev/golang.org/x/crypto/openpgp#SymmetricallyEncrypt for more details about the encryption
// configuration.
func NewWriter(writer io.Writer, passphrase []byte) (*Writer, error) {
	armorWriter, err := armor.Encode(writer, "PGP MESSAGE", nil)
	if err != nil {
		return nil, errors.Errorf("failure while encrypting the secret credentials: %s", err)
	}

	openGpgWriter, err := openpgp.SymmetricallyEncrypt(armorWriter, passphrase, nil, &packet.Config{
		DefaultCipher: packet.CipherAES256,
	})
	if err != nil {
		return nil, errors.Errorf("unable to encrypt the stream using PGP: %s", err)
	}

	tarWriter := tar.NewWriter(openGpgWriter)

	return &Writer{
		armorWriter:   armorWriter,
		openPgpWriter: openGpgWriter,
		tarballWriter: tarWriter,
	}, nil
}
