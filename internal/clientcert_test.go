// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// generateTestCertPEM creates a self-signed ECDSA certificate suitable for round-tripping through
// buildPKCS12 in tests. It returns PEM-encoded cert and PKCS#8 key.
func generateTestCertPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "eck-internal"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("creating cert: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func Test_buildPKCS12_RoundTrip(t *testing.T) {
	certPEM, keyPEM := generateTestCertPEM(t)
	const password = "s3cret"

	p12, err := buildPKCS12(certPEM, keyPEM, password)
	if err != nil {
		t.Fatalf("buildPKCS12: %v", err)
	}
	if len(p12) == 0 {
		t.Fatal("buildPKCS12 returned empty bytes")
	}

	priv, cert, _, err := pkcs12.DecodeChain(p12, password)
	if err != nil {
		t.Fatalf("decoding p12: %v", err)
	}
	if priv == nil || cert == nil {
		t.Fatalf("decoded p12 missing key or cert: priv=%v cert=%v", priv, cert)
	}
	if cert.Subject.CommonName != "eck-internal" {
		t.Errorf("unexpected CN: %s", cert.Subject.CommonName)
	}
}

func Test_buildPKCS12_RejectsBadPEM(t *testing.T) {
	if _, err := buildPKCS12([]byte("not a pem"), []byte("not a pem"), "x"); err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}

func Test_jobTemplate_keystore(t *testing.T) {
	tests := []struct {
		name               string
		keystoreSecretName string
		wantVolume         bool
		wantArg            string
	}{
		{
			name:               "no keystore: no volume, no pkiKeystore arg",
			keystoreSecretName: "",
			wantVolume:         false,
			wantArg:            "--pkiKeystore",
		},
		{ //nolint:gosec // G101 false positive: test fixture, not a credential
			name:               "with keystore: volume mounted and arg present",
			keystoreSecretName: "test-es-elasticsearch-diag-keystore",
			wantVolume:         true,
			wantArg:            "--pkiKeystore /client-cert/keystore.p12 --pkiPassText $KEYSTORE_PW",
		},
	}

	tpl, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := map[string]interface{}{
				"PodName":             "test-pod",
				"DiagnosticImage":     "docker.elastic.co/eck-dev/support-diagnostics:latest",
				"Namespace":           "default",
				"ESSecretName":        "es-secret",
				"ESSecretKey":         "elastic",
				"SVCName":             "elasticsearch-es-http",
				"Type":                "api",
				"TLS":                 true,
				"OutputDir":           "/diagnostic-output",
				"MainContainerName":   "stack-diagnostics",
				"KeystoreSecretName":  tt.keystoreSecretName,
				"KeystoreFile":        "keystore.p12",
				"KeystorePasswordKey": "password",
				"KeystoreMountPath":   "/client-cert",
			}

			buf := new(bytes.Buffer)
			if err := tpl.Execute(buf, data); err != nil {
				t.Fatalf("executing template: %v", err)
			}
			var pod corev1.Pod
			if err := yaml.Unmarshal(buf.Bytes(), &pod); err != nil {
				t.Fatalf("unmarshaling pod: %v\nrendered:\n%s", err, buf.String())
			}

			hasVolume := false
			for _, v := range pod.Spec.Volumes {
				if v.Name == "client-cert" {
					hasVolume = true
					if v.Secret == nil || v.Secret.SecretName != tt.keystoreSecretName {
						t.Errorf("client-cert volume has wrong secret: %+v", v.Secret)
					}
				}
			}
			if hasVolume != tt.wantVolume {
				t.Errorf("client-cert volume present = %v, want %v\nrendered:\n%s", hasVolume, tt.wantVolume, buf.String())
			}

			hasMount := false
			hasEnv := false
			for _, c := range pod.Spec.Containers {
				for _, m := range c.VolumeMounts {
					if m.Name == "client-cert" {
						hasMount = true
					}
				}
				for _, e := range c.Env {
					if e.Name == "KEYSTORE_PW" {
						hasEnv = true
					}
				}
			}
			if hasMount != tt.wantVolume {
				t.Errorf("client-cert mount present = %v, want %v", hasMount, tt.wantVolume)
			}
			if hasEnv != tt.wantVolume {
				t.Errorf("KEYSTORE_PW env present = %v, want %v", hasEnv, tt.wantVolume)
			}

			gotArgs := strings.Join(pod.Spec.Containers[0].Args, " ")
			if tt.wantVolume {
				if !strings.Contains(gotArgs, tt.wantArg) {
					t.Errorf("expected args to contain %q, got: %s", tt.wantArg, gotArgs)
				}
			} else {
				if strings.Contains(gotArgs, tt.wantArg) {
					t.Errorf("expected args NOT to contain %q, got: %s", tt.wantArg, gotArgs)
				}
			}
		})
	}
}
