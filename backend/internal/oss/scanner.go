package oss

import (
	"context"
	"fmt"
	"strings"
	"time"

	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	carbon "github.com/dromara/carbon/v2"
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

// ScanResult 单个账号的扫描结果。
//
// CoveredBuckets 只包含 ListCname 调用成功的 bucket——对账下线时必须以它为边界,
// 把扫描失败的 bucket 当成"域名不存在"会导致一次 API 抖动误杀整批域名。
type ScanResult struct {
	Domains        []DomainInfo
	CoveredBuckets []string
}

// Scanner 负责扫描阿里云 OSS bucket 和自定义域名
type Scanner struct {
	logger *zap.Logger
}

func NewScanner(logger *zap.Logger) *Scanner {
	return &Scanner{logger: logger}
}

// ScanAccount 扫描单个账号下所有 bucket 的自定义域名。
// 列 bucket 失败时返回 error(整个账号不可信);单个 bucket 的 ListCname 失败只是
// 不计入 CoveredBuckets,不影响其它 bucket。
func (s *Scanner) ScanAccount(ctx context.Context, account *ent.CloudAccount) (ScanResult, error) {
	client := newClient(account.AccessKeyID, account.AccessKeySecret, "cn-hangzhou")

	var result ScanResult
	p := client.NewListBucketsPaginator(&alioss.ListBucketsRequest{})
	for p.HasNext() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return ScanResult{}, fmt.Errorf("list buckets: %w", err)
		}
		for _, bucket := range page.Buckets {
			bucketName := alioss.ToString(bucket.Name)
			location := alioss.ToString(bucket.Location)
			region := strings.TrimPrefix(location, "oss-")

			bucketDomains, err := s.listBucketCnames(ctx, account, bucketName, region)
			if err != nil {
				s.logger.Warn("获取 bucket CNAME 失败,本轮不对该 bucket 下的域名做下线判定",
					zap.String("bucket", bucketName),
					zap.String("region", region),
					zap.Error(err),
				)
				continue
			}
			result.Domains = append(result.Domains, bucketDomains...)
			result.CoveredBuckets = append(result.CoveredBuckets, bucketName)
		}
	}
	return result, nil
}

func (s *Scanner) listBucketCnames(ctx context.Context, account *ent.CloudAccount, bucket, region string) ([]DomainInfo, error) {
	client := newClient(account.AccessKeyID, account.AccessKeySecret, region)

	result, err := client.ListCname(ctx, &alioss.ListCnameRequest{
		Bucket: new(bucket),
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

// CnameInfo 一条 OSS 自定义域名绑定的现状
type CnameInfo struct {
	Domain string
	Status string
	Cert   *OSSCertInfo
}

// FindCname 查询某个域名当前是否还绑定在该 bucket 上。
//
// 三种返回要分清楚:
//   - (info, nil)   域名确实绑着
//   - (nil, nil)    域名确实不在这个 bucket 上了
//   - (nil, err)    查不到,不能据此下任何结论
//
// 部署前拿它做预检:域名早已解绑时直接放弃,不必先去 Let's Encrypt 白签一张证书。
func (s *Scanner) FindCname(ctx context.Context, account *ent.CloudAccount, bucket, region, domain string) (*CnameInfo, error) {
	if bucket == "" {
		return nil, fmt.Errorf("域名 %s 没有关联的 bucket", domain)
	}
	client := newClient(account.AccessKeyID, account.AccessKeySecret, region)
	result, err := client.ListCname(ctx, &alioss.ListCnameRequest{
		Bucket: new(bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("list cname for %s: %w", bucket, err)
	}
	for _, cname := range result.Cnames {
		if alioss.ToString(cname.Domain) != domain {
			continue
		}
		info := &CnameInfo{
			Domain: domain,
			Status: alioss.ToString(cname.Status),
		}
		if cname.Certificate != nil {
			end := carbon.Parse(alioss.ToString(cname.Certificate.ValidEndDate))
			if !end.IsInvalid() && !end.IsZero() {
				start := carbon.Parse(alioss.ToString(cname.Certificate.ValidStartDate))
				info.Cert = &OSSCertInfo{
					ValidStartDate: start.StdTime(),
					ValidEndDate:   end.StdTime(),
				}
			}
		}
		return info, nil
	}
	return nil, nil
}

// GetDomainCert 查询指定域名在 OSS 侧的证书信息,查不到返回 nil
func (s *Scanner) GetDomainCert(ctx context.Context, account *ent.CloudAccount, bucket, region, domain string) *OSSCertInfo {
	info, err := s.FindCname(ctx, account, bucket, region, domain)
	if err != nil || info == nil {
		return nil
	}
	return info.Cert
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
