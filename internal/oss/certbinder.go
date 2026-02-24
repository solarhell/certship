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
		Bucket: alioss.Ptr(info.Bucket),
		BucketCnameConfiguration: &alioss.BucketCnameConfiguration{
			Cname: &alioss.Cname{
				Domain: alioss.Ptr(info.Domain),
				CertificateConfiguration: &alioss.CertificateConfiguration{
					Certificate: alioss.Ptr(certPEM),
					PrivateKey:  alioss.Ptr(keyPEM),
					Force:       alioss.Ptr(true),
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
