package discovery

import (
	"context"
	"errors"
	cdn "github.com/solarhell/certship/internal/cdn"
	"github.com/solarhell/certship/internal/oss"
	"github.com/solarhell/certship/pkg/ent"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestCloudInventory(t *testing.T) {
	a := &ent.CloudAccount{ID: "a", Name: "a", AccessKeyID: "a"}
	b := &ent.CloudAccount{ID: "b", Name: "b", AccessKeyID: "b"}
	base := func() sources {
		return sources{
			scanOSS:      func(context.Context, *ent.CloudAccount) (oss.ScanResult, error) { return oss.ScanResult{}, nil },
			listCDN:      func(string, string) ([]cdn.DomainInfo, error) { return nil, nil },
			describeCert: func(string, string, string) (*cdn.CertInfo, error) { return nil, nil },
		}
	}
	t.Run("successful empty inventory contains no deleted resources", func(t *testing.T) {
		result := run(context.Background(), zap.NewNop(), []*ent.CloudAccount{a}, base())
		if !result.Complete() || result.Coverage.IsEmpty() || len(result.Domains) != 0 {
			t.Fatalf("empty successful inventory: %+v", result)
		}
		if _, ok := result.Lookup("deleted.example.com"); ok {
			t.Fatal("deleted resource survived fresh inventory")
		}
	})
	t.Run("empty enabled account list", func(t *testing.T) {
		if r := run(context.Background(), zap.NewNop(), nil, base()); !r.Complete() || len(r.Domains) != 0 {
			t.Fatal("disabled accounts must produce an empty resource list")
		}
	})
	t.Run("listing failure is unknown not empty", func(t *testing.T) {
		source := base()
		source.scanOSS = func(context.Context, *ent.CloudAccount) (oss.ScanResult, error) {
			return oss.ScanResult{}, errors.New("access denied")
		}
		result := run(context.Background(), zap.NewNop(), []*ent.CloudAccount{a}, source)
		if result.Complete() {
			t.Fatal("failed listing reported as complete empty inventory")
		}
	})
	t.Run("failed bucket query does not make complete inventory", func(t *testing.T) {
		source := base()
		source.scanOSS = func(context.Context, *ent.CloudAccount) (oss.ScanResult, error) {
			return oss.ScanResult{Buckets: []string{"private"}}, nil
		}
		if result := run(context.Background(), zap.NewNop(), []*ent.CloudAccount{a}, source); result.Complete() {
			t.Fatal("CNAME failure was silently omitted")
		}
	})
	t.Run("unavailable CDN does not downgrade to OSS", func(t *testing.T) {
		source := base()
		source.scanOSS = func(_ context.Context, acct *ent.CloudAccount) (oss.ScanResult, error) {
			return oss.ScanResult{Buckets: []string{"live"}, CoveredBuckets: []string{"live"}, Domains: []oss.DomainInfo{{Domain: "test.example.com", Bucket: "live", Account: acct, CertKnown: true}}}, nil
		}
		source.listCDN = func(string, string) ([]cdn.DomainInfo, error) { return nil, errors.New("timeout") }
		result := run(context.Background(), zap.NewNop(), []*ent.CloudAccount{a}, source)
		info, ok := result.Lookup("test.example.com")
		if !ok || result.Deployable(info) || result.Complete() {
			t.Fatal("partial scan incorrectly authorized OSS deployment")
		}
	})
	t.Run("certificate lookup failure is not absent certificate", func(t *testing.T) {
		source := base()
		source.listCDN = func(string, string) ([]cdn.DomainInfo, error) {
			return []cdn.DomainInfo{{Domain: "test.example.com", SSLOn: true}}, nil
		}
		source.describeCert = func(string, string, string) (*cdn.CertInfo, error) { return nil, errors.New("throttled") }
		result := run(context.Background(), zap.NewNop(), []*ent.CloudAccount{a}, source)
		info, _ := result.Lookup("test.example.com")
		if info.CertKnown || result.Complete() {
			t.Fatal("certificate read failure was interpreted as missing certificate")
		}
	})
	for _, accounts := range [][]*ent.CloudAccount{{a, b}, {b, a}} {
		t.Run("cross account CDN wins regardless of scan order "+accounts[0].Name, func(t *testing.T) {
			source := base()
			source.scanOSS = func(_ context.Context, acct *ent.CloudAccount) (oss.ScanResult, error) {
				if acct.ID != "a" {
					return oss.ScanResult{}, nil
				}
				return oss.ScanResult{Buckets: []string{"origin"}, CoveredBuckets: []string{"origin"}, Domains: []oss.DomainInfo{{Domain: "test.example.com", Bucket: "origin", Region: "cn-hangzhou", Account: acct, CertKnown: true}}}, nil
			}
			source.listCDN = func(id, _ string) ([]cdn.DomainInfo, error) {
				if id != "b" {
					return nil, nil
				}
				return []cdn.DomainInfo{{Domain: "test.example.com", SourceBucket: "origin", SourceRegion: "cn-hangzhou", SSLOn: true}}, nil
			}
			source.describeCert = func(string, string, string) (*cdn.CertInfo, error) {
				return &cdn.CertInfo{ValidEndDate: time.Now().Add(90 * 24 * time.Hour)}, nil
			}
			result := run(context.Background(), zap.NewNop(), accounts, source)
			info, ok := result.Lookup("test.example.com")
			if !ok || !result.Complete() || info.Account.ID != "b" || info.OSSAccount.ID != "a" || info.OSSBucket != "origin" || info.Origin != OriginBoth || info.DeployTarget() != DeployTargetCDN || info.CloudCert() == nil {
				t.Fatalf("wrong deployment ownership: %+v", info)
			}
		})
	}
}
