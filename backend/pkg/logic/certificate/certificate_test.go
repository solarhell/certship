package certificate

import (
	"testing"
	"time"

	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/pkg/ent"
)

func TestCurrentCertificatesUsesCloudFactsOnly(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	future := now.Add(365 * 24 * time.Hour)
	states := map[string]*ent.Domain{
		"live.example.com":    {ID: "live", Domain: "live.example.com", ExpiresAt: &future, Managed: false, RetryCount: 2},
		"deleted.example.com": {ID: "deleted", Domain: "deleted.example.com", Managed: true},
	}
	snapshot := discovery.Result{ObservedAt: now, Domains: []discovery.DomainInfo{{Domain: "live.example.com", Bucket: "new-origin", Region: "cn-hangzhou", Account: &ent.CloudAccount{Name: "current-account"}, Origin: discovery.OriginCDN, CertKnown: true, CDNCert: &discovery.CertInfo{ValidStartDate: now.Add(-24 * time.Hour), ValidEndDate: now.Add(12 * time.Hour)}}}}
	items := currentCertificates(snapshot, states)
	if len(items) != 1 {
		t.Fatal("historical DB resources leaked into cloud list")
	}
	item := items[0]
	if item.Bucket != "new-origin" || item.AccountName != "current-account" || item.DeployTarget != "cdn" || item.ExpiresAt != now.Add(12*time.Hour).Format(time.RFC3339) {
		t.Fatalf("DB cloud snapshot overrode current facts: %+v", item)
	}
	if item.Managed || item.RetryCount != 2 || item.Id != "live" {
		t.Fatal("local intent/state was lost")
	}
	if len(currentCertificates(discovery.Result{}, states)) != 0 {
		t.Fatal("empty cloud list fell back to DB")
	}
}
func TestCurrentCertificatePaginationOrder(t *testing.T) {
	now := time.Now()
	account := &ent.CloudAccount{Name: "account"}
	states := map[string]*ent.Domain{"a": {ID: "a"}, "b": {ID: "b"}, "c": {ID: "c"}}
	snapshot := discovery.Result{Domains: []discovery.DomainInfo{{Domain: "c", Account: account}, {Domain: "b", Account: account, OSSCert: &discovery.CertInfo{ValidEndDate: now}}, {Domain: "a", Account: account, OSSCert: &discovery.CertInfo{ValidEndDate: now}}}}
	items := currentCertificates(snapshot, states)
	if items[0].Id != "c" || items[1].Id != "a" || items[2].Id != "b" {
		t.Fatalf("unstable order: %v", items)
	}
	if snapshot.Domains[0].Domain != "c" || snapshot.Domains[1].Domain != "b" {
		t.Fatal("listing mutated shared cloud snapshot")
	}
}
