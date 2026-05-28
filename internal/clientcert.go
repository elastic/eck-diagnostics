// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"

	"software.sslmate.com/src/go-pkcs12"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// clientCertSecretSuffix matches cloud-on-k8s OperatorClientCertSecretName layout for the ES namer:
	// "<esName>-es-internal-client-cert".
	clientCertSecretSuffix = "-es-internal-client-cert" //nolint:gosec // G101 false positive: secret name suffix, not a credential

	// clientCertFile / clientKeyFile match cloud-on-k8s certificates.CertFileName / KeyFileName.
	clientCertFile = "tls.crt"
	clientKeyFile  = "tls.key"

	// keystoreFile is the PKCS12 entry name inside the per-job Secret mounted into the diagnostic Pod.
	keystoreFile = "keystore.p12"
	// keystorePasswordKey is the Secret key holding the PKCS12 password.
	keystorePasswordKey = "password"
	// keystoreMountPath is where the per-job Secret is mounted inside the diagnostic Pod.
	keystoreMountPath = "/client-cert"
	// keystoreSecretSuffix is appended to the diagnostic Pod name to derive the per-job keystore Secret name.
	keystoreSecretSuffix = "-keystore"
)

// loadOperatorClientCert returns the PEM-encoded client cert and key from the operator-managed
// secret <esName>-es-internal-client-cert if it exists. Returns (nil, nil, false, nil) if the
// secret does not exist (i.e. mTLS is not configured). This mirrors the lookup performed by
// cloud-on-k8s certificates.LoadOperatorClientCertIfExists.
func loadOperatorClientCert(k *Kubectl, ns, esName string) (certPEM, keyPEM []byte, found bool, err error) {
	secretName := esName + clientCertSecretSuffix
	secret, err := k.CoreV1().Secrets(ns).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("getting client cert secret %s/%s: %w", ns, secretName, err)
	}
	cert, ok := secret.Data[clientCertFile]
	if !ok || len(cert) == 0 {
		return nil, nil, false, fmt.Errorf("missing %s in secret %s/%s", clientCertFile, ns, secretName)
	}
	key, ok := secret.Data[clientKeyFile]
	if !ok || len(key) == 0 {
		return nil, nil, false, fmt.Errorf("missing %s in secret %s/%s", clientKeyFile, ns, secretName)
	}
	return cert, key, true, nil
}

// buildPKCS12 produces a PKCS12 keystore protected by password from the given PEM-encoded
// certificate and private key.
func buildPKCS12(certPEM, keyPEM []byte, password string) ([]byte, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("decoding certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("decoding private key PEM")
	}
	priv, err := parsePrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	return pkcs12.Modern.Encode(priv, cert, nil, password)
}

// parsePrivateKey accepts PKCS#8, PKCS#1 (RSA) or SEC1 (EC) DER-encoded keys.
func parsePrivateKey(der []byte) (any, error) {
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(der); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("unsupported private key format")
}

// createKeystoreSecret builds a PKCS12 keystore from certPEM/keyPEM and stores it together with
// a freshly-generated password in a per-job Secret named <podName>-keystore. Any pre-existing
// Secret with the same name is replaced. The returned name is suitable for mounting into the
// diagnostic Pod.
func createKeystoreSecret(ctx context.Context, k *Kubectl, ns, podName string, certPEM, keyPEM []byte) (string, error) {
	pwBytes := make([]byte, 16)
	if _, err := rand.Read(pwBytes); err != nil {
		return "", fmt.Errorf("generating keystore password: %w", err)
	}
	password := hex.EncodeToString(pwBytes)

	p12, err := buildPKCS12(certPEM, keyPEM, password)
	if err != nil {
		return "", fmt.Errorf("building keystore: %w", err)
	}

	secretName := podName + keystoreSecretSuffix
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/name": "eck-diagnostics",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			keystoreFile:        p12,
			keystorePasswordKey: []byte(password),
		},
	}

	if err := deleteKeystoreSecret(ctx, k, ns, secretName); err != nil {
		return "", err
	}
	if _, err := k.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("creating keystore secret %s/%s: %w", ns, secretName, err)
	}
	return secretName, nil
}

// deleteKeystoreSecret deletes the per-job keystore secret if it exists. Not-found errors are ignored.
func deleteKeystoreSecret(ctx context.Context, k *Kubectl, ns, secretName string) error {
	if secretName == "" {
		return nil
	}
	if err := k.CoreV1().Secrets(ns).Delete(ctx, secretName, metav1.DeleteOptions{GracePeriodSeconds: ptr.To[int64](0)}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting keystore secret %s/%s: %w", ns, secretName, err)
	}
	return nil
}
