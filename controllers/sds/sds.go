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

// Package sds implements the SDS (Secret Discovery Service) controller.
package sds

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tentens-tech/xds-controller/pkg/xds"
	xdserr "github.com/tentens-tech/xds-controller/pkg/xds/err"
	xdstypes "github.com/tentens-tech/xds-controller/pkg/xds/types"
)

// ErrK8sClientNotConfigured is returned when trying to use Kubernetes storage without a configured client
var ErrK8sClientNotConfigured = xdserr.ErrK8sClientNotConfigured

var defaultDuration time.Duration = 10 * time.Minute

// GetSecret retrieves a secret for the given domain.
func GetSecret(ctx context.Context, domain *xdstypes.DomainConfig, x *xds.Config) (*auth.Secret, time.Duration, time.Time, error) {
	log := ctrllog.FromContext(ctx)
	var cg xds.CertGetter

	// Determine the storage type based on the configuration
	if domain.Challenge != nil {
		// Let's Encrypt challenge requires storage for certificates
		// Use Vault if configured, otherwise fall back to Kubernetes secrets
		switch {
		case domain.Config.VaultStorageConfig != nil:
			domain.Config.Type = xdstypes.Vault
		case x.Storage.VaultStorageConfig != nil:
			domain.Config.Type = xdstypes.Vault
		default:
			// Fall back to Kubernetes secrets when no Vault config is specified
			domain.Config.Type = xdstypes.Kubernetes
		}
	} else {
		// Self-signed certificate mode - use Local storage by default
		// unless Kubernetes storage is explicitly requested or no local path is specified
		switch {
		case domain.Config.KubernetesStorageConfig != nil:
			domain.Config.Type = xdstypes.Kubernetes
		case domain.Config.LocalStorageConfig != nil && domain.Config.LocalStorageConfig.Path != "":
			domain.Config.Type = xdstypes.Local
		case x.K8sClient != nil:
			// Fall back to Kubernetes if k8s client is available and no local config
			domain.Config.Type = xdstypes.Kubernetes
		default:
			domain.Config.Type = xdstypes.Local
		}
	}

	switch domain.Config.Type {
	case xdstypes.Local:
		cg = xds.NewSelfSigned()

	case xdstypes.Kubernetes:
		// Use Kubernetes secrets for storage
		if x.K8sClient == nil {
			return nil, defaultDuration, time.Time{}, xdserr.ErrK8sClientNotConfigured
		}

		// Initialize Kubernetes storage config if not present
		if domain.Config.KubernetesStorageConfig == nil {
			domain.Config.KubernetesStorageConfig = &xdstypes.KubernetesStorageConfig{
				Namespace:  x.DefaultNamespace,
				SecretName: domain.SecretName,
			}
		}

		// Determine cert getter: Let's Encrypt if challenge is specified, otherwise self-signed
		if domain.Challenge != nil {
			if x.LeClient == nil {
				x.LeClient = xds.NewLEA(
					x.LetsEncryptAccount.Email,
					x.LetsEncryptAccount.PrivateKeyBase64,
				)
			}
			cg = x.LeClient
		} else {
			cg = xds.NewSelfSigned()
		}

	case xdstypes.Vault:
		switch {
		case domain.Config.VaultStorageConfig != nil:
			s := xds.StorageConfig{
				StorageConfig: xdstypes.StorageConfig{
					Type: xdstypes.Vault,
					VaultStorageConfig: &xdstypes.VaultStorageConfig{
						Path:          domain.Config.VaultStorageConfig.Path,
						URL:           domain.Config.VaultStorageConfig.URL,
						Token:         domain.Config.VaultStorageConfig.Token,
						SkipTLSVerify: domain.Config.VaultStorageConfig.SkipTLSVerify,
					},
				},
			}
			log.V(2).Info("Vault config found in domain config. Using it.")
			if err := s.InitStorage(x); err != nil {
				return nil, defaultDuration, time.Time{}, err
			}
		case x.Storage.VaultStorageConfig != nil:
			s := xds.StorageConfig{
				StorageConfig: xdstypes.StorageConfig{
					Type: xdstypes.Vault,
					VaultStorageConfig: &xdstypes.VaultStorageConfig{
						Path:          x.Storage.VaultStorageConfig.Path,
						URL:           x.Storage.VaultStorageConfig.URL,
						Token:         x.Storage.VaultStorageConfig.Token,
						SkipTLSVerify: x.Storage.VaultStorageConfig.SkipTLSVerify,
					},
				},
			}

			domain.Config.VaultStorageConfig = &xdstypes.VaultStorageConfig{
				Path:          x.Storage.VaultStorageConfig.Path,
				URL:           x.Storage.VaultStorageConfig.URL,
				Token:         x.Storage.VaultStorageConfig.Token,
				SkipTLSVerify: x.Storage.VaultStorageConfig.SkipTLSVerify,
			}
			log.V(2).Info("Vault config found only in global config. Using it.")
			if err := s.InitStorage(x); err != nil {
				return nil, defaultDuration, time.Time{}, err
			}
		default:
			return nil, defaultDuration, time.Time{}, xdserr.ErrVaultNotConfigured
		}

		// Use existing client or create new one
		if x.LeClient == nil {
			x.LeClient = xds.NewLEA(
				x.LetsEncryptAccount.Email,
				x.LetsEncryptAccount.PrivateKeyBase64,
			)
		}
		cg = x.LeClient
	}

	secret, generateBeforeExpiration, expireTime, err := makeSecret(ctx, domain, x, cg, x.IsLeaderInstance())
	if err != nil {
		return nil, generateBeforeExpiration, expireTime, err
	}

	return secret, generateBeforeExpiration, expireTime, err
}

func makeSecret(ctx context.Context, d *xdstypes.DomainConfig, x xds.GlobalConfig, cg xds.CertGetter, isLeader bool) (*auth.Secret, time.Duration, time.Time, error) {
	log := ctrllog.FromContext(ctx)
	s := &auth.Secret{Name: d.SecretName}
	var cert xdstypes.Cert
	log.V(2).Info("checking certificate secret", "secretName", d.SecretName)
	cert, err := x.StorageConfigRead(d)
	if len(cert.Priv) != 0 && len(cert.Pub) != 0 {
		s = &auth.Secret{
			Name: d.SecretName,
			Type: &auth.Secret_TlsCertificate{
				TlsCertificate: &auth.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Pub},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Priv},
					},
				},
			},
		}
	}

	if errors.Is(err, xdserr.ErrVaultNotConfigured) {
		return nil, defaultDuration, time.Time{}, err
	}
	if errors.Is(err, xdserr.ErrCertNotFound) || errors.Is(err, xdserr.ErrCertNil) {
		if !isLeader {
			log.V(2).Info("certificate not found, leader will take over", "secretName", d.SecretName)
			return nil, defaultDuration, time.Time{}, err
		}

		if d.Challenge != nil && d.Challenge.ChallengeType == xdstypes.MANUAL {
			log.V(2).Info("certificate not found and is manual, skipping", "secretName", d.SecretName)
			return s, defaultDuration, time.Time{}, nil
		}

		log.V(0).Info("certificate not found, starting to generate", "secretName", d.SecretName)
		if x.GetDryRun() {
			log.V(0).Info("dry run mode is enabled, skipping", "secretName", d.SecretName)
			return s, defaultDuration, time.Time{}, nil
		}

		// For Vault storage, create empty cert to check if we can write before expensive LE operations
		// Skip this for Kubernetes storage to avoid creating empty secrets
		if d.Config.Type == xdstypes.Vault {
			err = x.StorageConfigWrite(d, xdstypes.Cert{})
			if errors.Is(err, xdserr.ErrVaultNotConfigured) {
				log.V(0).Info(err.Error())
				return nil, defaultDuration, time.Time{}, err
			} else if err != nil {
				return s, defaultDuration, time.Time{}, fmt.Errorf(xdserr.ErrWriteCert.Error()+": %w", err)
			}
		}

		cert, err = cg.Get(d)
		if err != nil {
			if strings.Contains(err.Error(), "zone could not be found") {
				log.V(0).Info(err.Error())
				return s, defaultDuration, time.Time{}, nil
			}

			if strings.Contains(err.Error(), xdserr.ErrCustomEnvReplace.Error()) {
				log.V(0).Info(err.Error())
				return s, defaultDuration, time.Time{}, nil
			}

			if strings.Contains(err.Error(), "is not set") {
				log.V(0).Info(err.Error())
				return s, defaultDuration, time.Time{}, nil
			}

			if errors.Is(err, xdserr.ErrServiceBusy) {
				return s, defaultDuration, time.Time{}, nil
			}

			if errors.Is(err, xdserr.ErrRateLimited) {
				return s, 120 * time.Minute, time.Time{}, nil
			}

			return s, defaultDuration, time.Time{}, fmt.Errorf(xdserr.ErrGetCert.Error()+": %w", err)
		}
		if x.GetDryRun() {
			log.V(0).Info("dry run mode is enabled, skipping", "secretName", d.SecretName)
			return s, defaultDuration, time.Time{}, nil
		}
		err = x.StorageConfigWrite(d, cert)
		if errors.Is(err, xdserr.ErrVaultNotConfigured) {
			log.V(0).Info(err.Error())
			return nil, defaultDuration, time.Time{}, err
		} else if err != nil {
			return s, defaultDuration, time.Time{}, fmt.Errorf(xdserr.ErrWriteCert.Error()+": %w", err)
		}
	} else if err != nil {
		return s, defaultDuration, time.Time{}, fmt.Errorf(xdserr.ErrReadCert.Error()+": %w", err)
	}

	var renewBeforeExpireMinutes = float64(x.GetRenewBeforeExpireInMinutes())

	x509Cert, err := get509Cert(cert)
	if err != nil {
		return s, defaultDuration, time.Time{}, err
	}

	forceRegenerate := false
	certEnv := xdstypes.Production
	if strings.Contains(
		x509Cert.Issuer.CommonName,
		strings.ToUpper(string(xdstypes.Staging)),
	) {
		certEnv = xdstypes.Staging
	}

	// acme env change force regenerate
	if d.Challenge != nil && d.Challenge.ACMEEnv != certEnv && d.Challenge.ChallengeType != xdstypes.MANUAL {
		forceRegenerate = true
	}

	// domain count change force regenerate
	if len(x509Cert.DNSNames) != len(d.Domains) && d.Challenge != nil && d.Challenge.ChallengeType != xdstypes.MANUAL {
		forceRegenerate = true
	}

	if d.Config.Type == xdstypes.Local {
		forceRegenerate = false
	}
	minutesUntilExpire := time.Until(x509Cert.NotAfter).Minutes()

	if forceRegenerate || (minutesUntilExpire <= renewBeforeExpireMinutes && (d.Challenge == nil || d.Challenge.ChallengeType != xdstypes.MANUAL)) {
		if !isLeader {
			log.V(0).Info("certificate is about to expire, leader will take over", "secretName", d.SecretName)
			return &auth.Secret{
				Name: d.SecretName,
				Type: &auth.Secret_TlsCertificate{
					TlsCertificate: &auth.TlsCertificate{
						CertificateChain: &core.DataSource{
							Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Pub},
						},
						PrivateKey: &core.DataSource{
							Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Priv},
						},
					},
				},
			}, 5 * time.Minute, x509Cert.NotAfter, nil
		}
		log.V(0).Info("starting to renew certificate", "secretName", d.SecretName)
		if x.GetDryRun() {
			log.V(0).Info("dry run mode is enabled, skipping", "secretName", d.SecretName)
			return s, defaultDuration, time.Time{}, nil
		}

		if len(cert.Priv) == 0 && len(cert.Pub) == 0 {
			return s, defaultDuration, time.Time{}, xdserr.ErrBadKeyData
		}

		// For Vault storage, check if we can write before expensive LE operations
		if d.Config.Type == xdstypes.Vault {
			err = x.StorageConfigWrite(d, cert)
			if errors.Is(err, xdserr.ErrVaultNotConfigured) {
				log.V(0).Info(err.Error())
				return nil, defaultDuration, time.Time{}, err
			}
		}

		oldSecret := &auth.Secret{
			Name: d.SecretName,
			Type: &auth.Secret_TlsCertificate{
				TlsCertificate: &auth.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Pub},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Priv},
					},
				},
			},
		}

		cert, err = cg.Get(d)
		if err != nil {
			if strings.Contains(err.Error(), "zone could not be found") {
				return oldSecret, defaultDuration, time.Time{}, err
			}

			if strings.Contains(err.Error(), xdserr.ErrCustomEnvReplace.Error()) {
				return oldSecret, defaultDuration, time.Time{}, err
			}

			if strings.Contains(err.Error(), "is not set") {
				return oldSecret, defaultDuration, time.Time{}, err
			}

			if errors.Is(err, xdserr.ErrServiceBusy) {
				return oldSecret, defaultDuration, time.Time{}, xdserr.ErrServiceBusy
			}

			if errors.Is(err, xdserr.ErrRateLimited) {
				return oldSecret, 120 * time.Minute, time.Time{}, xdserr.ErrRateLimited
			}

			return oldSecret, defaultDuration, time.Time{}, fmt.Errorf(xdserr.ErrGetCert.Error()+": %w", err)
		}
		err = x.StorageConfigWrite(d, cert)
		if errors.Is(err, xdserr.ErrVaultNotConfigured) {
			log.V(0).Info(err.Error())
			return s, defaultDuration, time.Time{}, err
		} else if err != nil {
			return s, defaultDuration, time.Time{}, fmt.Errorf(xdserr.ErrWriteCert.Error()+": %w", err)
		}
		x509Cert, err = get509Cert(cert)
		if err != nil {
			return s, defaultDuration, time.Time{}, err
		}

		minutesUntilExpire = time.Until(x509Cert.NotAfter).Minutes()

	} else {
		log.V(2).Info("certificate is valid", "secretName", d.SecretName)
	}

	for _, domain := range d.Domains {
		xds.RecordCertificateMetrics(d.SecretName, domain, minutesUntilExpire)
	}

	minutesUntilExpiration := int(minutesUntilExpire - renewBeforeExpireMinutes)
	generateBeforeExpiration := time.Duration(minutesUntilExpiration) * time.Minute

	if len(cert.Priv) == 0 && len(cert.Pub) == 0 {
		return s, defaultDuration, time.Time{}, xdserr.ErrBadKeyData
	}

	// return secret and time remaining until expiration
	return &auth.Secret{
		Name: d.SecretName,
		Type: &auth.Secret_TlsCertificate{
			TlsCertificate: &auth.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Pub},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{InlineBytes: cert.Priv},
				},
			},
		},
	}, generateBeforeExpiration, x509Cert.NotAfter, nil
}

func get509Cert(cert xdstypes.Cert) (*x509.Certificate, error) {
	block, _ := pem.Decode(cert.Pub)
	if block == nil {
		return nil, xdserr.ErrBadKeyData
	}
	if block.Type != "CERTIFICATE" {
		return nil, xdserr.ErrCertType
	}

	// Decode the public key
	return x509.ParseCertificate(block.Bytes)
}
