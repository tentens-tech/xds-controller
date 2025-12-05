// Copyright 2025 The Envoy XDS Controller Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xds

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	legolog "github.com/go-acme/lego/v4/log"
	"github.com/go-acme/lego/v4/providers/dns"
	"github.com/go-acme/lego/v4/registration"
	vault "github.com/hashicorp/vault/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tentens-tech/xds-controller/pkg/logger"
	xdserr "github.com/tentens-tech/xds-controller/pkg/xds/err"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

// LetsEncrypt implements acme.User for Let's Encrypt ACME operations.
type LetsEncrypt struct {
	Email        string
	Registration *registration.Resource
	Key          crypto.PrivateKey
	client       *lego.Client
}

type CertGetter interface {
	Get(d *xdstypes.DomainConfig) (xdstypes.Cert, error)
}

func (u *LetsEncrypt) GetEmail() string {
	return u.Email
}
func (u LetsEncrypt) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *LetsEncrypt) GetPrivateKey() crypto.PrivateKey {
	return u.Key
}

// initClient initializes the ACME client and performs registration
func (u *LetsEncrypt) initClient(caDirURL string) error {
	config := lego.NewConfig(u)
	config.CADirURL = caDirURL
	config.Certificate.KeyType = certcrypto.RSA2048

	// Set our custom logger
	legolog.Logger = logger.NewLegoLogger()

	client, err := lego.NewClient(config)
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}

	// If we have an existing registration, try to verify it
	if u.Registration != nil {
		// Query the registration to check if it's still valid
		_, err := client.Registration.QueryRegistration()
		if err != nil {
			// If there's an error, the registration might be invalid
			// Clear it so we can create a new one
			log.Log.V(0).Info("existing registration invalid, will create new one", "error", err)
			u.Registration = nil
		}
	}

	// Register if we don't have a valid registration
	if u.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			if strings.Contains(err.Error(), "Service busy") {
				return xdserr.ErrServiceBusy
			}
			return fmt.Errorf("lets encrypt register: %w", err)
		}
		u.Registration = reg
		log.Log.V(0).Info("created new ACME registration", "email", u.Email)
	}

	u.client = client
	return nil
}

func (u *LetsEncrypt) Get(d *xdstypes.DomainConfig) (xdstypes.Cert, error) {
	// Initialize client if not already initialized or if CADirURL has changed
	if u.client == nil {
		if err := u.initClient(d.Challenge.ACMEEnv.String()); err != nil {
			return xdstypes.Cert{}, err
		}
	}

	// Verify client is still valid, reinitialize if needed
	if _, err := u.client.Registration.QueryRegistration(); err != nil {
		log.Log.V(1).Info("registration validation failed, reinitializing client", "error", err)
		if err := u.initClient(d.Challenge.ACMEEnv.String()); err != nil {
			return xdstypes.Cert{}, err
		}
	}

	switch d.Challenge.ChallengeType {
	case xdstypes.HTTP01:
		err := u.client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "5002"))
		if err != nil {
			return xdstypes.Cert{}, fmt.Errorf("new http provider: %w", err)
		}
	case xdstypes.DNS01:
		var envsToRestore []string

		if len(d.Challenge.CustomDNS01ProviderEnvReplace)%2 == 0 {
			for i := 0; i < len(d.Challenge.CustomDNS01ProviderEnvReplace); i += 2 {
				envsToRestore = append(envsToRestore, os.Getenv(d.Challenge.CustomDNS01ProviderEnvReplace[i+1]))
			}
			// check if the first env variable is set and not empty
			for i := 0; i < len(d.Challenge.CustomDNS01ProviderEnvReplace); i += 2 {
				if os.Getenv(d.Challenge.CustomDNS01ProviderEnvReplace[i]) == "" {
					return xdstypes.Cert{}, fmt.Errorf("env variable %s is not set", d.Challenge.CustomDNS01ProviderEnvReplace[i])
				}
			}

			for i := 0; i < len(d.Challenge.CustomDNS01ProviderEnvReplace); i += 2 {
				t := os.Getenv(d.Challenge.CustomDNS01ProviderEnvReplace[i])
				_ = os.Setenv(d.Challenge.CustomDNS01ProviderEnvReplace[i+1], t) //nolint:errcheck // env var setup for DNS provider
			}

			defer func() {
				c := 0
				for i := 0; i < len(d.Challenge.CustomDNS01ProviderEnvReplace); i += 2 {
					_ = os.Setenv(d.Challenge.CustomDNS01ProviderEnvReplace[i+1], envsToRestore[c]) //nolint:errcheck // restoring original env vars
					c++
				}
			}()

		} else {
			return xdstypes.Cert{}, xdserr.ErrCustomEnvReplace
		}

		provider, err := dns.NewDNSChallengeProviderByName(d.Challenge.DNS01Provider)
		if err != nil {
			return xdstypes.Cert{}, fmt.Errorf("new dns01 provider: %w", err)
		}

		err = u.client.Challenge.SetDNS01Provider(provider)
		if err != nil {
			return xdstypes.Cert{}, fmt.Errorf("set dns01 provider: %w", err)
		}
	}

	request := certificate.ObtainRequest{
		Domains: d.Domains,
		Bundle:  true,
	}

	certificates, err := u.client.Certificate.Obtain(request)
	if err != nil {
		if strings.Contains(err.Error(), "Service busy") {
			return xdstypes.Cert{}, xdserr.ErrServiceBusy
		} else if strings.Contains(err.Error(), "rateLimited") {
			return xdstypes.Cert{}, xdserr.ErrRateLimited
		}
		return xdstypes.Cert{}, fmt.Errorf("lets encrypt obtain: %w", err)
	}

	return xdstypes.Cert{
		Pub:  certificates.Certificate,
		Priv: certificates.PrivateKey,
	}, nil
}

func NewLEA(e, k string) *LetsEncrypt {
	var privateKey crypto.PrivateKey
	var keyErr error

	if k != "" {
		keyBytes, decodeErr := base64.StdEncoding.DecodeString(k)
		if decodeErr != nil {
			log.Log.Error(decodeErr, "unable to decode private key")
			return nil
		}
		block, _ := pem.Decode(keyBytes)
		if block == nil {
			log.Log.Error(nil, "unable to decode PEM block containing private key")
			return nil
		}
		privateKey, keyErr = x509.ParsePKCS1PrivateKey(block.Bytes)
		if keyErr != nil {
			log.Log.Error(keyErr, "unable to parse private key")
			return nil
		}
	} else {
		privateKey, keyErr = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if keyErr != nil {
			log.Log.Error(keyErr, "unable to generate private key")
			return nil
		}
	}

	return &LetsEncrypt{
		Email: e,
		Key:   privateKey,
	}
}

type SelfSigned struct{}

func NewSelfSigned() *SelfSigned {
	return &SelfSigned{}
}

func (c *SelfSigned) Get(d *xdstypes.DomainConfig) (xdstypes.Cert, error) {
	notBefore := time.Now()

	expireAfterDays := 365 // default to one year
	if d.Config.LocalStorageConfig != nil && d.Config.LocalStorageConfig.ExpireAfterDays != 0 {
		expireAfterDays = d.Config.LocalStorageConfig.ExpireAfterDays
	}

	notAfter := notBefore.Add(time.Duration(expireAfterDays) * 24 * time.Hour) // Certificate validity as specified in config
	notAfterCA := notBefore.Add(time.Duration(1024) * 24 * time.Hour)          // CA certificate validity

	var caCert *x509.Certificate
	var caPriv *rsa.PrivateKey

	if d.Config.LocalStorageConfig != nil && d.Config.Base64CACert != "" && d.Config.Base64CAKey != "" {
		// If the variables are set, decode and parse them
		caCertPem, decodeErr := base64.StdEncoding.DecodeString(d.Config.Base64CACert)
		if decodeErr != nil {
			return xdstypes.Cert{}, fmt.Errorf("failed to decode Base64CACert: %w", decodeErr)
		}
		caKeyPem, decodeErr := base64.StdEncoding.DecodeString(d.Config.Base64CAKey)
		if decodeErr != nil {
			return xdstypes.Cert{}, fmt.Errorf("failed to decode Base64CAKey: %w", decodeErr)
		}

		// Parse the PEM blocks
		caCertBlock, _ := pem.Decode(caCertPem)
		if caCertBlock == nil {
			return xdstypes.Cert{}, fmt.Errorf("failed to decode CA certificate PEM block")
		}
		caKeyBlock, _ := pem.Decode(caKeyPem)
		if caKeyBlock == nil {
			return xdstypes.Cert{}, fmt.Errorf("failed to decode CA key PEM block")
		}

		// Parse the certificate and the private key
		var parseErr error
		caCert, parseErr = x509.ParseCertificate(caCertBlock.Bytes)
		if parseErr != nil {
			return xdstypes.Cert{}, parseErr
		}
		caPrivKey, parseErr := x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes)
		if parseErr != nil {
			return xdstypes.Cert{}, parseErr
		}
		caPriv = caPrivKey
	} else {
		// If the variables are not set, generate a new CA
		var genErr error
		caPriv, genErr = rsa.GenerateKey(rand.Reader, 2048)
		if genErr != nil {
			return xdstypes.Cert{}, genErr
		}

		caTemplate := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject: pkix.Name{
				CommonName: "Root CA", // CA common name
			},
			NotBefore:             notBefore,
			NotAfter:              notAfterCA,
			BasicConstraintsValid: true,
			IsCA:                  true,
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			SignatureAlgorithm:    x509.SHA256WithRSA,
		}

		caBytes, createErr := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPriv.PublicKey, caPriv)
		if createErr != nil {
			return xdstypes.Cert{}, createErr
		}

		var parseErr error
		caCert, parseErr = x509.ParseCertificate(caBytes)
		if parseErr != nil {
			return xdstypes.Cert{}, parseErr
		}
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return xdstypes.Cert{}, err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128) // Generate a 128-bit number
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return xdstypes.Cert{}, err
	}

	// This is the new certificate's template
	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: d.Domains[0], // Use the first domain as the Common Name
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		IsCA:                  false, // This is not a CA certificate
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		SignatureAlgorithm:    x509.SHA256WithRSA,
		DNSNames:              d.Domains, // Include all the domains in the SAN (Subject Alternative Name)
	}

	// Create the new certificate, signed by the CA's private key
	derBytes, err := x509.CreateCertificate(rand.Reader, &certTemplate, caCert, &priv.PublicKey, caPriv)
	if err != nil {
		return xdstypes.Cert{}, err
	}

	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return xdstypes.Cert{Pub: certPem, Priv: privPem}, nil
}

type StorageConfig struct {
	xdstypes.StorageConfig
}

func (cf *StorageConfig) InitStorage(c *Config) error {
	var err error

	switch cf.Type {
	case xdstypes.Vault:
		config := vault.DefaultConfig()
		config.Address = cf.VaultStorageConfig.URL
		config.HttpClient = c.HTTPClient
		c.VaultClient, err = vault.NewClient(config)
		if err != nil {
			return fmt.Errorf("[Vault] unable to initialize client: %w", err)
		}

		c.VaultClient.SetToken(cf.Token)

		vaultURL, err := url.JoinPath(cf.VaultStorageConfig.URL, "/v1", cf.VaultStorageConfig.Path, "config") // nolint
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, vaultURL, http.NoBody)
		if err != nil {
			return err
		}
		req.Header.Add("X-Vault-Token", cf.Token)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return fmt.Errorf("[Vault] error to access path %s: %w", cf.VaultStorageConfig.Path, err)
		} else if resp.StatusCode != 200 {
			return fmt.Errorf("[Vault] error to get vault path %s\n response code: %d", cf.VaultStorageConfig.Path, resp.StatusCode)
		}

		defer func() { _ = resp.Body.Close() }() //nolint:errcheck // closing body in defer
		defer c.HTTPClient.CloseIdleConnections()

	case xdstypes.Local:
		return nil

	case xdstypes.Kubernetes:
		// Kubernetes storage uses the K8sClient from Config which should be set by the controller
		if c.K8sClient == nil {
			return xdserr.ErrK8sClientNotConfigured
		}
		return nil

	default:
		return fmt.Errorf("storage type invalid")
	}
	return nil
}

type GlobalConfig interface {
	GetDomainConfigs() map[string][]*xdstypes.DomainConfig
	GetRndNumber() int
	GetRenewBeforeExpireInMinutes() int
	GetDryRun() bool
	StorageConfigReaderWriter
}

func (c *Config) GetDomainConfigs() map[string][]*xdstypes.DomainConfig {
	return c.DomainConfigs
}

func (c *Config) GetRndNumber() int {
	return c.RndNumber
}

func (c *Config) GetRenewBeforeExpireInMinutes() int {
	return c.RenewBeforeExpireInMinutes
}

func (c *Config) GetDryRun() bool {
	return c.DryRun
}

type StorageConfigReaderWriter interface {
	StorageConfigRead(d *xdstypes.DomainConfig) (xdstypes.Cert, error)
	StorageConfigWrite(d *xdstypes.DomainConfig, c xdstypes.Cert) error
}

func (c *Config) StorageConfigRead(d *xdstypes.DomainConfig) (xdstypes.Cert, error) {
	var cert xdstypes.Cert
	var readErr error

	switch d.Config.Type {
	case xdstypes.Local:
		path := "./"
		if d.Config.LocalStorageConfig != nil && d.Config.LocalStorageConfig.Path != "" {
			path = d.Config.LocalStorageConfig.Path
		}
		pubPath := filepath.Join(path, d.SecretName+".crt")  // #nosec G304 -- path is from trusted configuration
		privPath := filepath.Join(path, d.SecretName+".key") // #nosec G304 -- path is from trusted configuration
		if _, statErr := os.Stat(pubPath); os.IsNotExist(statErr) {
			return cert, xdserr.ErrCertNotFound
		}
		if _, statErr := os.Stat(privPath); os.IsNotExist(statErr) {
			return cert, xdserr.ErrCertNotFound
		}

		cert.Pub, readErr = os.ReadFile(pubPath) // #nosec G304 -- path is from trusted configuration
		if os.IsNotExist(readErr) {
			return cert, xdserr.ErrCertNotFound
		} else if readErr != nil {
			return cert, readErr
		}

		cert.Priv, readErr = os.ReadFile(privPath) // #nosec G304 -- path is from trusted configuration
		if os.IsNotExist(readErr) {
			return cert, xdserr.ErrCertNotFound
		} else if readErr != nil {
			return cert, readErr
		}

		if len(cert.Pub) == 0 || len(cert.Priv) == 0 {
			return cert, xdserr.ErrCertNil
		}

	case xdstypes.Vault:
		if d.Config.VaultStorageConfig == nil {
			return cert, xdserr.ErrVaultNotConfigured
		}

		if d == nil {
			return cert, fmt.Errorf("domain config is nil")
		}

		// throttle the read 200 ms
		time.Sleep(time.Duration(200) * time.Millisecond)
		secret, vaultErr := c.VaultClient.KVv2(d.Config.VaultStorageConfig.Path).Get(context.Background(), d.SecretName)
		if vaultErr != nil {
			if strings.Contains(vaultErr.Error(), "secret not found") {
				return cert, xdserr.ErrCertNotFound
			}

			return cert, fmt.Errorf("unable to read secret: %w", vaultErr)
		}

		if secret.Data["Pub"] == nil || secret.Data["Priv"] == nil {
			return cert, xdserr.ErrCertNil
		}

		pubb64, ok := secret.Data["Pub"].(string)
		if !ok {
			return cert, fmt.Errorf("value type assertion failed: %T %#v", secret.Data["Pub"], secret.Data["Pub"])
		}

		pub, decodeErr := base64.StdEncoding.DecodeString(pubb64)
		if decodeErr != nil {
			return cert, fmt.Errorf("unable to decode public key: %w", decodeErr)
		}
		cert.Pub = pub

		privb64, ok := secret.Data["Priv"].(string)
		if !ok {
			return cert, fmt.Errorf("value type assertion failed: %T %#v", secret.Data["Priv"], secret.Data["Priv"])
		}

		priv, decodeErr := base64.StdEncoding.DecodeString(privb64)
		if decodeErr != nil {
			return cert, fmt.Errorf("unable to decode private key: %w", decodeErr)
		}
		cert.Priv = priv

		return cert, nil

	case xdstypes.Kubernetes:
		if c.K8sClient == nil {
			return cert, xdserr.ErrK8sClientNotConfigured
		}

		namespace := c.DefaultNamespace
		secretName := d.SecretName
		if d.Config.KubernetesStorageConfig != nil {
			if d.Config.KubernetesStorageConfig.Namespace != "" {
				namespace = d.Config.KubernetesStorageConfig.Namespace
			}
			if d.Config.KubernetesStorageConfig.SecretName != "" {
				secretName = d.Config.KubernetesStorageConfig.SecretName
			}
		}

		var k8sSecret corev1.Secret
		if getErr := c.K8sClient.Get(context.Background(), k8stypes.NamespacedName{
			Name:      secretName,
			Namespace: namespace,
		}, &k8sSecret); getErr != nil {
			if apierrors.IsNotFound(getErr) {
				return cert, xdserr.ErrCertNotFound
			}
			return cert, fmt.Errorf("unable to read kubernetes secret: %w", getErr)
		}

		// Check if it's a TLS secret type
		if k8sSecret.Type == corev1.SecretTypeTLS {
			cert.Pub = k8sSecret.Data[corev1.TLSCertKey]
			cert.Priv = k8sSecret.Data[corev1.TLSPrivateKeyKey]
		} else {
			// Fallback to Opaque secret with Pub/Priv keys
			cert.Pub = k8sSecret.Data["tls.crt"]
			cert.Priv = k8sSecret.Data["tls.key"]
		}

		if len(cert.Pub) == 0 || len(cert.Priv) == 0 {
			return cert, xdserr.ErrCertNil
		}

		return cert, nil

	default:
		return cert, fmt.Errorf("unknown storage type")
	}
	return cert, readErr
}

func (c *Config) StorageConfigWrite(d *xdstypes.DomainConfig, cert xdstypes.Cert) error {
	if c.DryRun {
		return nil
	}
	switch d.Config.Type {
	case xdstypes.Local:
		path := "./"
		if d.Config.LocalStorageConfig != nil && d.Config.LocalStorageConfig.Path != "" {
			path = d.Config.LocalStorageConfig.Path
		}

		// Create the directory if it doesn't exist
		if mkdirErr := os.MkdirAll(path, 0o750); mkdirErr != nil { // #nosec G301 -- 0750 is acceptable for cert directories
			return mkdirErr
		}

		if writeErr := os.WriteFile(filepath.Join(path, d.SecretName+".crt"), cert.Pub, 0o600); writeErr != nil {
			return writeErr
		}
		if writeErr := os.WriteFile(filepath.Join(path, d.SecretName+".key"), cert.Priv, 0o600); writeErr != nil {
			return writeErr
		}

	case xdstypes.Vault:
		if d.Config.VaultStorageConfig == nil {
			return xdserr.ErrVaultNotConfigured
		}

		certificateData := map[string]interface{}{
			"Pub":  cert.Pub,
			"Priv": cert.Priv,
		}
		_, putErr := c.VaultClient.KVv2(d.Config.VaultStorageConfig.Path).Put(context.Background(), d.SecretName, certificateData)
		if putErr != nil {
			// Check if the error is due to the mount not existing (404)
			if strings.Contains(putErr.Error(), "no handler for route") || strings.Contains(putErr.Error(), "404") {
				// Try to create the KV v2 mount
				mountErr := c.VaultClient.Sys().Mount(d.Config.VaultStorageConfig.Path, &vault.MountInput{
					Type:        "kv",
					Description: "Auto-created KV v2 secrets engine for envoy-xds-controller",
					Options: map[string]string{
						"version": "2",
					},
				})
				if mountErr != nil {
					return fmt.Errorf("unable to write secret and failed to create KV mount: write error: %w, mount error: %w", putErr, mountErr)
				}
				// Retry the write after creating the mount
				_, retryErr := c.VaultClient.KVv2(d.Config.VaultStorageConfig.Path).Put(context.Background(), d.SecretName, certificateData)
				if retryErr != nil {
					return fmt.Errorf("unable to write secret after creating mount: %w", retryErr)
				}
			} else {
				return fmt.Errorf("unable to write secret: %w", putErr)
			}
		}

	case xdstypes.Kubernetes:
		if c.K8sClient == nil {
			return xdserr.ErrK8sClientNotConfigured
		}

		namespace := c.DefaultNamespace
		secretName := d.SecretName
		if d.Config.KubernetesStorageConfig != nil {
			if d.Config.KubernetesStorageConfig.Namespace != "" {
				namespace = d.Config.KubernetesStorageConfig.Namespace
			}
			if d.Config.KubernetesStorageConfig.SecretName != "" {
				secretName = d.Config.KubernetesStorageConfig.SecretName
			}
		}

		ctx := context.Background()

		// Try to get existing secret first
		var existingSecret corev1.Secret
		getErr := c.K8sClient.Get(ctx, k8stypes.NamespacedName{
			Name:      secretName,
			Namespace: namespace,
		}, &existingSecret)

		k8sSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "xds-controller",
					"envoyxds.io/secret-name":      d.SecretName,
				},
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       cert.Pub,
				corev1.TLSPrivateKeyKey: cert.Priv,
			},
		}

		switch {
		case apierrors.IsNotFound(getErr):
			// Create new secret
			if createErr := c.K8sClient.Create(ctx, k8sSecret); createErr != nil {
				return fmt.Errorf("unable to create kubernetes secret: %w", createErr)
			}
		case getErr != nil:
			return fmt.Errorf("unable to get kubernetes secret: %w", getErr)
		default:
			// Update existing secret
			existingSecret.Data = k8sSecret.Data
			existingSecret.Type = corev1.SecretTypeTLS
			if existingSecret.Labels == nil {
				existingSecret.Labels = make(map[string]string)
			}
			existingSecret.Labels["app.kubernetes.io/managed-by"] = "xds-controller"
			existingSecret.Labels["envoyxds.io/secret-name"] = d.SecretName

			if updateErr := c.K8sClient.Update(ctx, &existingSecret); updateErr != nil {
				return fmt.Errorf("unable to update kubernetes secret: %w", updateErr)
			}
		}

	default:
		return fmt.Errorf("unknown storage type")
	}
	return nil
}
