package credentialsEncrypter

import (
	"archive/tar"
	"bytes"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/errors"
	"io"
	"io/ioutil"
)

// NewReader returns a pointer to a tar.Reader. This reader automatically decrypts the passed reader, assuming it is a
// tarball symmetrically encrypted with OpenPGP.
func NewReader(reader io.Reader, passphrase []byte) (*tar.Reader, error) {
	decoder, err := armor.Decode(reader)
	if err != nil {
		return nil, err
	}
	firstTime := true
	passGenerator := func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
		if firstTime == true {
			firstTime = false
			return passphrase, nil
		} else {
			return nil, errors.ErrKeyIncorrect
		}
	}
	message, err := openpgp.ReadMessage(decoder.Body, nil, passGenerator, nil)
	if err != nil {
		return nil, err
	}
	return tar.NewReader(message.UnverifiedBody), nil
}

// Reads the content of the currently cursor in the tar reader and returns it as an array of bytes. Note, you still
// have to call tarReader.Next() in order to iterate over all the tarball files.
func ReadFile(tarReader *tar.Reader) ([]byte, error) {
	contentBuffer := &bytes.Buffer{}
	if _, err := io.Copy(contentBuffer, tarReader); err != nil {
		return nil, err
	}
	bs, err := ioutil.ReadAll(contentBuffer)
	if err != nil {
		return nil, err
	}
	return bs, nil
}
