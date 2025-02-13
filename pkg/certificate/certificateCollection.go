/*
 * Copyright 2018 Venafi, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package certificate

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/Venafi/vcert/v4/pkg/verror"
	"strings"
)

//ChainOption represents the options to be used with the certificate chain
type ChainOption int

const (
	//ChainOptionRootLast specifies the root certificate should be in the last position of the chain
	ChainOptionRootLast ChainOption = iota
	//ChainOptionRootFirst specifies the root certificate should be in the first position of the chain
	ChainOptionRootFirst
	//ChainOptionIgnore specifies the chain should be ignored
	ChainOptionIgnore
)

//ChainOptionFromString converts the string to the corresponding ChainOption
func ChainOptionFromString(order string) ChainOption {
	switch strings.ToLower(order) {
	case "root-first":
		return ChainOptionRootFirst
	case "ignore":
		return ChainOptionIgnore
	default:
		return ChainOptionRootLast
	}
}

//PEMCollection represents a collection of PEM data
type PEMCollection struct {
	Certificate string   `json:",omitempty"`
	PrivateKey  string   `json:",omitempty"`
	Chain       []string `json:",omitempty"`
	CSR         string   `json:",omitempty"`
}

//NewPEMCollection creates a PEMCollection based on the data being passed in
func NewPEMCollection(certificate *x509.Certificate, privateKey crypto.Signer, privateKeyPassword []byte, format ...string) (*PEMCollection, error) {
	collection := PEMCollection{}
	currentFormat := ""
	if len(format) > 0 && format[0] != "" {
		currentFormat = format[0]
	}
	if certificate != nil {
		collection.Certificate = string(pem.EncodeToMemory(GetCertificatePEMBlock(certificate.Raw)))
	}
	if privateKey != nil {
		var p *pem.Block
		var err error
		if len(privateKeyPassword) > 0 {
			p, err = GetEncryptedPrivateKeyPEMBock(privateKey, privateKeyPassword, currentFormat)
		} else {
			p, err = GetPrivateKeyPEMBock(privateKey, currentFormat)
		}
		if err != nil {
			return nil, err
		}
		collection.PrivateKey = string(pem.EncodeToMemory(p))
	}
	return &collection, nil
}

//PEMCollectionFromBytes creates a PEMCollection based on the data passed in
func PEMCollectionFromBytes(certBytes []byte, chainOrder ChainOption) (*PEMCollection, error) {
	var (
		current    []byte
		remaining  []byte
		p          *pem.Block
		cert       *x509.Certificate
		chain      []*x509.Certificate
		privPEM    string
		err        error
		collection *PEMCollection
	)
	current = certBytes

	for {
		p, remaining = pem.Decode(current)
		if p == nil {
			break
		}
		switch p.Type {
		case "CERTIFICATE":
			cert, err = x509.ParseCertificate(p.Bytes)
			if err != nil {
				return nil, err
			}
			chain = append(chain, cert)
		case "RSA PRIVATE KEY", "EC PRIVATE KEY", "ENCRYPTED PRIVATE KEY", "PRIVATE KEY":
			privPEM = string(current)
		}
		current = remaining
	}

	if len(chain) > 0 {
		switch chainOrder {
		case ChainOptionRootFirst:
			collection, err = NewPEMCollection(chain[len(chain)-1], nil, nil)
			if len(chain) > 1 && chainOrder != ChainOptionIgnore {
				for _, caCert := range chain[:len(chain)-1] {
					err = collection.AddChainElement(caCert)
					if err != nil {
						return nil, err
					}
				}
			}
		default:
			collection, err = NewPEMCollection(chain[0], nil, nil)
			if len(chain) > 1 && chainOrder != ChainOptionIgnore {
				for _, caCert := range chain[1:] {
					err = collection.AddChainElement(caCert)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
	} else {
		collection = &PEMCollection{}
	}
	collection.PrivateKey = privPEM

	return collection, nil
}

//AddPrivateKey adds a Private Key to the PEMCollection. Note that the collection can only contain one private key
func (col *PEMCollection) AddPrivateKey(privateKey crypto.Signer, privateKeyPassword []byte, format ...string) error {

	currentFormat := ""
	if len(format) > 0 && format[0] != "" {
		currentFormat = format[0]
	}

	if col.PrivateKey != "" {
		return fmt.Errorf("%w: the PEM Collection can only contain one private key", verror.VcertError)
	}
	var p *pem.Block
	var err error
	if len(privateKeyPassword) > 0 {
		p, err = GetEncryptedPrivateKeyPEMBock(privateKey, privateKeyPassword, currentFormat)
	} else {
		p, err = GetPrivateKeyPEMBock(privateKey, currentFormat)
	}
	if err != nil {
		return err
	}
	col.PrivateKey = string(pem.EncodeToMemory(p))
	return nil
}

//AddChainElement adds a chain element to the collection
func (col *PEMCollection) AddChainElement(certificate *x509.Certificate) error {
	if certificate == nil {
		return fmt.Errorf("%w: certificate cannot be nil", verror.VcertError)
	}
	pemChain := col.Chain
	pemChain = append(pemChain, string(pem.EncodeToMemory(GetCertificatePEMBlock(certificate.Raw))))
	col.Chain = pemChain
	return nil
}

func (col *PEMCollection) ToTLSCertificate() tls.Certificate {
	cert := tls.Certificate{}
	b, _ := pem.Decode([]byte(col.Certificate))
	cert.Certificate = append(cert.Certificate, b.Bytes)
	for _, c := range col.Chain {
		b, _ := pem.Decode([]byte(c))
		cert.Certificate = append(cert.Certificate, b.Bytes)
	}
	b, _ = pem.Decode([]byte(col.PrivateKey))

	switch b.Type {
	case "EC PRIVATE KEY":
		cert.PrivateKey, _ = x509.ParseECPrivateKey(b.Bytes)
	case "RSA PRIVATE KEY":
		var privKey interface{}
		privKey, err := x509.ParsePKCS1PrivateKey(b.Bytes)
		if err != nil {
			privKey, _ = x509.ParsePKCS8PrivateKey(b.Bytes)
		}
		cert.PrivateKey = privKey
	}
	return cert
}
