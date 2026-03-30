package oss

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dromara/carbon/v2"
	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"go.uber.org/zap"

	"github.com/solarhell/certship/pkg/ent"
)

// OSSCertInfo 从 OSS ListCname 接口读取的现有证书信息
type OSSCertInfo struct {
	ValidStartDate time.Time
	ValidEndDate   time.Time
}

// DomainInfo 描述一个 OSS bucket 绑定的自定义域名及其所属账号信息
type DomainInfo struct {
	Domain  string
	Bucket  string
	Region  string // 如 "cn-hangzhou"
	Account *ent.CloudAccount
	// OSSCert 为 nil 表示该域名在 OSS 侧未绑定证书
	OSSCert *OSSCertInfo
}

// Scanner 负责扫描阿里云 OSS bucket 和自定义域名
type Scanner struct {
	logger *zap.Logger
}

func NewScanner(logger *zap.Logger) *Scanner {
	return &Scanner{logger: logger}
}

// ScanAll 扫描所有账号下的 OSS bucket，返回所有绑定的自定义域名
func (s *Scanner) ScanAll(ctx context.Context, accounts []*ent.CloudAccount) []DomainInfo {
	var all []DomainInfo
	for _, account := range accounts {
		domains, err := s.ScanAccount(ctx, account)
		if err != nil {
			s.logger.Error("扫描账号失败",
				zap.String("account", account.Name),
				zap.Error(err),
			)
			continue
		}
		s.logger.Info("账号扫描完成",
			zap.String("account", account.Name),
			zap.Int("domain_count", len(domains)),
		)
		all = append(all, domains...)
	}
	return all
}

// ScanAccount 扫描单个账号下所有 bucket 的自定义域名
func (s *Scanner) ScanAccount(ctx context.Context, account *ent.CloudAccount) ([]DomainInfo, error) {
	client := newClient(account.AccessKeyID, account.AccessKeySecret, "cn-hangzhou")

	var domains []DomainInfo
	p := client.NewListBucketsPaginator(&alioss.ListBucketsRequest{})
	for p.HasNext() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list buckets: %w", err)
		}
		for _, bucket := range page.Buckets {
			bucketName := alioss.ToString(bucket.Name)
			location := alioss.ToString(bucket.Location)
			region := strings.TrimPrefix(location, "oss-")

			bucketDomains, err := s.listBucketCnames(ctx, account, bucketName, region)
			if err != nil {
				s.logger.Warn("获取 bucket CNAME 失败",
					zap.String("bucket", bucketName),
					zap.String("region", region),
					zap.Error(err),
				)
				continue
			}
			domains = append(domains, bucketDomains...)
		}
	}
	return domains, nil
}

func (s *Scanner) listBucketCnames(ctx context.Context, account *ent.CloudAccount, bucket, region string) ([]DomainInfo, error) {
	client := newClient(account.AccessKeyID, account.AccessKeySecret, region)

	result, err := client.ListCname(ctx, &alioss.ListCnameRequest{
		Bucket: alioss.Ptr(bucket),
	})
	if err != nil {
		return nil, err
	}

	var domains []DomainInfo
	for _, cname := range result.Cnames {
		domain := alioss.ToString(cname.Domain)
		status := alioss.ToString(cname.Status)
		if domain == "" || status != "Enabled" {
			if domain != "" {
				s.logger.Debug("跳过非 Enabled 状态的 CNAME",
					zap.String("domain", domain),
					zap.String("status", status),
				)
			}
			continue
		}

		info := DomainInfo{
			Domain:  domain,
			Bucket:  bucket,
			Region:  region,
			Account: account,
		}

		// 读取 OSS 侧已绑定的证书信息
		if cname.Certificate != nil {
			end := carbon.Parse(alioss.ToString(cname.Certificate.ValidEndDate))
			if !end.IsInvalid() && !end.IsZero() {
				start := carbon.Parse(alioss.ToString(cname.Certificate.ValidStartDate))
				info.OSSCert = &OSSCertInfo{
					ValidStartDate: start.StdTime(),
					ValidEndDate:   end.StdTime(),
				}
			}
		}

		domains = append(domains, info)
	}
	return domains, nil
}

// GetDomainCert 查询指定域名在 OSS 侧的证书信息
func (s *Scanner) GetDomainCert(ctx context.Context, account *ent.CloudAccount, bucket, region, domain string) *OSSCertInfo {
	client := newClient(account.AccessKeyID, account.AccessKeySecret, region)
	result, err := client.ListCname(ctx, &alioss.ListCnameRequest{
		Bucket: alioss.Ptr(bucket),
	})
	if err != nil {
		return nil
	}
	for _, cname := range result.Cnames {
		if alioss.ToString(cname.Domain) != domain {
			continue
		}
		if cname.Certificate == nil {
			return nil
		}
		end := carbon.Parse(alioss.ToString(cname.Certificate.ValidEndDate))
		if end.IsInvalid() || end.IsZero() {
			return nil
		}
		start := carbon.Parse(alioss.ToString(cname.Certificate.ValidStartDate))
		return &OSSCertInfo{
			ValidStartDate: start.StdTime(),
			ValidEndDate:   end.StdTime(),
		}
	}
	return nil
}

func newClient(accessKeyID, accessKeySecret, region string) *alioss.Client {
	cfg := alioss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKeyID,
			accessKeySecret,
			"",
		)).
		WithRegion(region)
	return alioss.NewClient(cfg)
}
