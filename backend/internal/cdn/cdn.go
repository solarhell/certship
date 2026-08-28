package cdn

import (
	"errors"
	"fmt"
	"time"

	cdn "github.com/alibabacloud-go/cdn-20180510/v9/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	dara "github.com/alibabacloud-go/tea/dara"
	tea "github.com/alibabacloud-go/tea/tea"
	carbon "github.com/dromara/carbon/v2"
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
		AccessKeyId:     new(accessKeyID),
		AccessKeySecret: new(accessKeySecret),
		Endpoint:        new("cdn.aliyuncs.com"),
	}
	return cdn.NewClient(config)
}

// errDomainNotFound 是阿里云对"域名不在该账号 CDN 下"返回的错误码
const errDomainNotFound = "InvalidDomain.NotFound"

// CheckOnline 检查域名当前是否是该账号下处于 online 状态的 CDN 加速域名。
//
// 返回 (false, nil) 表示确认域名不在或未上线;返回 error 表示查不到,
// 调用方不能把它当成"域名不存在"——限流和权限问题都会走到这里。
func (m *Manager) CheckOnline(accessKeyID, accessKeySecret, domain string) (bool, error) {
	client, err := newClient(accessKeyID, accessKeySecret)
	if err != nil {
		return false, fmt.Errorf("创建 CDN 客户端失败: %w", err)
	}

	req := &cdn.DescribeCdnDomainDetailRequest{}
	req.SetDomainName(domain)

	resp, err := client.DescribeCdnDomainDetail(req)
	if err != nil {
		if isDomainNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("查询 CDN 域名 %s 详情失败: %w", domain, err)
	}
	if resp.Body == nil || resp.Body.GetDomainDetailModel == nil {
		return false, nil
	}
	return deref(resp.Body.GetDomainDetailModel.DomainStatus) == "online", nil
}

// isDomainNotFound 判断错误是否为"域名不存在"
func isDomainNotFound(err error) bool {
	if teaErr, ok := errors.AsType[*tea.SDKError](err); ok {
		return deref(teaErr.Code) == errDomainNotFound
	}
	if daraErr, ok := errors.AsType[*dara.SDKError](err); ok {
		return deref(daraErr.Code) == errDomainNotFound
	}
	return false
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
		// 读不到就不写:重新部署本身不是幂等无代价的操作,
		// 因为一次查询抖动就去覆盖线上证书,比晚一个周期发现问题更糟
		m.logger.Warn("查询 CDN 证书信息失败,本轮跳过重新部署", zap.String("domain", domain), zap.Error(err))
		return false
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
