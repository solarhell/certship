package oss

import (
	"context"
	"fmt"

	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"go.uber.org/zap"
)

// CertBinder 负责将 SSL 证书绑定到 OSS bucket 的自定义域名
type CertBinder struct {
	logger *zap.Logger
}

func NewCertBinder(logger *zap.Logger) *CertBinder {
	return &CertBinder{logger: logger}
}

// BindCert 将 PEM 格式的证书和私钥绑定到 OSS bucket 的指定自定义域名
func (b *CertBinder) BindCert(ctx context.Context, info DomainInfo, certPEM, keyPEM string) error {
	client := newClient(info.Account.AccessKeyID, info.Account.AccessKeySecret, info.Region)

	_, err := client.PutCname(ctx, &alioss.PutCnameRequest{
		Bucket: new(info.Bucket),
		BucketCnameConfiguration: &alioss.BucketCnameConfiguration{
			Cname: &alioss.Cname{
				Domain: new(info.Domain),
				CertificateConfiguration: &alioss.CertificateConfiguration{
					Certificate: new(certPEM),
					PrivateKey:  new(keyPEM),
					Force:       new(true),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("put cname with cert for %s/%s: %w", info.Bucket, info.Domain, err)
	}

	b.logger.Info("证书已绑定到 OSS 自定义域名",
		zap.String("bucket", info.Bucket),
		zap.String("domain", info.Domain),
		zap.String("region", info.Region),
	)
	return nil
}

// DeleteCert 删除 OSS bucket 自定义域名上绑定的证书
func (b *CertBinder) DeleteCert(ctx context.Context, info DomainInfo) error {
	client := newClient(info.Account.AccessKeyID, info.Account.AccessKeySecret, info.Region)

	_, err := client.PutCname(ctx, &alioss.PutCnameRequest{
		Bucket: new(info.Bucket),
		BucketCnameConfiguration: &alioss.BucketCnameConfiguration{
			Cname: &alioss.Cname{
				Domain: new(info.Domain),
				CertificateConfiguration: &alioss.CertificateConfiguration{
					DeleteCertificate: new(true),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("delete cert for %s/%s: %w", info.Bucket, info.Domain, err)
	}

	b.logger.Info("已删除 OSS 自定义域名上的证书",
		zap.String("bucket", info.Bucket),
		zap.String("domain", info.Domain),
	)
	return nil
}
