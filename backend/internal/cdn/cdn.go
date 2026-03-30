package cdn

import (
	"fmt"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	cdn "github.com/alibabacloud-go/cdn-20180510/v9/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/dromara/carbon/v2"
	"go.uber.org/zap"
)

// Manager 负责阿里云 CDN 域名检测和证书部署
type Manager struct {
	logger *zap.Logger
}

func NewManager(logger *zap.Logger) *Manager {
	return &Manager{logger: logger}
}

func newClient(accessKeyID, accessKeySecret string) (*cdn.Client, error) {
	config := &openapi.Config{
		AccessKeyId:     dara.String(accessKeyID),
		AccessKeySecret: dara.String(accessKeySecret),
		Endpoint:        dara.String("cdn.aliyuncs.com"),
	}
	return cdn.NewClient(config)
}

// IsCDNDomain 调用 DescribeCdnDomainDetail 判断域名是否是 CDN 加速域名
func (m *Manager) IsCDNDomain(accessKeyID, accessKeySecret, domain string) bool {
	client, err := newClient(accessKeyID, accessKeySecret)
	if err != nil {
		m.logger.Warn("创建 CDN 客户端失败", zap.Error(err))
		return false
	}

	req := &cdn.DescribeCdnDomainDetailRequest{}
	req.SetDomainName(domain)

	_, err = client.DescribeCdnDomainDetail(req)
	if err != nil {
		return false
	}

	m.logger.Debug("检测到 CDN 域名", zap.String("domain", domain))
	return true
}

// NeedDeploy 检查 CDN 域名的证书是否过期或未配置，需要重新部署
func (m *Manager) NeedDeploy(accessKeyID, accessKeySecret, domain string) bool {
	client, err := newClient(accessKeyID, accessKeySecret)
	if err != nil {
		return false
	}

	req := &cdn.DescribeDomainCertificateInfoRequest{}
	req.SetDomainName(domain)

	resp, err := client.DescribeDomainCertificateInfo(req)
	if err != nil {
		m.logger.Warn("查询 CDN 证书信息失败", zap.String("domain", domain), zap.Error(err))
		return true // 查询失败时保守地认为需要部署
	}

	if resp.Body == nil || resp.Body.CertInfos == nil || len(resp.Body.CertInfos.CertInfo) == 0 {
		return true // 没有证书信息
	}

	certInfo := resp.Body.CertInfos.CertInfo[0]

	// 检查 SSL 是否开启
	if certInfo.ServerCertificateStatus != nil && *certInfo.ServerCertificateStatus != "on" {
		return true
	}

	// 检查证书是否过期
	if certInfo.CertExpireTime != nil {
		expiry := carbon.Parse(*certInfo.CertExpireTime)
		if !expiry.IsInvalid() && expiry.StdTime().Before(time.Now()) {
			m.logger.Info("CDN 域名证书已过期", zap.String("domain", domain), zap.String("expire_time", *certInfo.CertExpireTime))
			return true
		}
	}

	return false
}

// DeployCert 调用 SetCdnDomainSSLCertificate 部署证书到 CDN 域名
func (m *Manager) DeployCert(accessKeyID, accessKeySecret, domain, certPEM, keyPEM string) error {
	client, err := newClient(accessKeyID, accessKeySecret)
	if err != nil {
		return fmt.Errorf("创建 CDN 客户端失败: %w", err)
	}

	req := &cdn.SetCdnDomainSSLCertificateRequest{}
	req.SetDomainName(domain)
	req.SetSSLProtocol("on")
	req.SetCertType("upload")
	req.SetSSLPub(certPEM)
	req.SetSSLPri(keyPEM)

	_, err = client.SetCdnDomainSSLCertificate(req)
	if err != nil {
		return fmt.Errorf("部署证书到 CDN 域名 %s 失败: %w", domain, err)
	}

	m.logger.Info("证书已部署到 CDN 域名", zap.String("domain", domain))
	return nil
}
