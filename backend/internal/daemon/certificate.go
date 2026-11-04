package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/pkg/ent"
)

func applyTarget(dom *ent.Domain, info discovery.DomainInfo) {
	dom.Bucket, dom.Region, dom.AccountName = info.Bucket, info.Region, info.Account.Name
	dom.Origin, dom.DeployTarget = info.Origin, info.DeployTarget()
}

// Local material must validate independently of cloud observation dates in DB.
func reusableCertificate(domain, certPEM, keyPEM string, threshold time.Time) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("invalid local certificate/key pair: %w", err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if err := cert.VerifyHostname(domain); err != nil {
		return nil, err
	}
	if cert.NotBefore.After(time.Now()) || !cert.NotAfter.After(threshold) {
		return nil, fmt.Errorf("local certificate is not currently usable through renewal threshold")
	}
	return cert, nil
}

func saveCertificate(ctx context.Context, dom *ent.Domain, certPEM, keyPEM string) error {
	cert, err := reusableCertificate(dom.Domain, certPEM, keyPEM, time.Now())
	if err != nil {
		return err
	}
	if err := dom.Update().SetCertPem(certPEM).SetKeyPem(keyPEM).
		SetIssuedAt(cert.NotBefore).SetExpiresAt(cert.NotAfter).Exec(ctx); err != nil {
		return fmt.Errorf("保存签发材料失败，停止部署: %w", err)
	}
	dom.CertPem, dom.KeyPem, dom.IssuedAt, dom.ExpiresAt = certPEM, keyPEM, &cert.NotBefore, &cert.NotAfter
	return nil
}
