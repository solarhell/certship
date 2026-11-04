package daemon

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/solarhell/certship/internal/config"
	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/internal/management"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	"github.com/solarhell/certship/pkg/ent/renewtask"
	"go.uber.org/zap"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"
)

func testMaterial(t *testing.T, domain string, expiry time.Time) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
}
func TestReusableCertificateValidatesMaterial(t *testing.T) {
	cert, key := testMaterial(t, "test.example.com", time.Now().Add(90*24*time.Hour))
	_, other := testMaterial(t, "test.example.com", time.Now().Add(90*24*time.Hour))
	if _, err := reusableCertificate("test.example.com", cert, key, time.Now().Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		name, domain, cert, key string
		threshold               time.Time
	}{
		{"wrong private key", "test.example.com", cert, other, time.Now()},
		{"wrong hostname", "other.example.com", cert, key, time.Now()},
		{"expiry below threshold", "test.example.com", cert, key, time.Now().Add(100 * 24 * time.Hour)},
		{"corrupt material", "test.example.com", "invalid", key, time.Now()},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, err := reusableCertificate(bad.domain, bad.cert, bad.key, bad.threshold); err == nil {
				t.Fatal("unusable material was accepted")
			}
		})
	}
}

// Run with CERTSHIP_TEST_POSTGRES_DSN pointing to an expendable local server.
// This creates and removes its own database; no Alibaba Cloud or ACME calls.
func TestDatabaseCompatibilityAndDeploymentRetry(t *testing.T) {
	dsn := os.Getenv("CERTSHIP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CERTSHIP_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := fmt.Sprintf("certship_test_%d", os.Getpid())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)") })
	oldCfg := cfg.Copy()
	oldCfg.Database = name
	old, err := pgx.ConnectConfig(ctx, oldCfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(ctx, "CREATE TABLE public.t_domain (id varchar PRIMARY KEY, domain varchar NOT NULL, account_name varchar NOT NULL); INSERT INTO public.t_domain VALUES ('legacy', 'legacy.example.com', 'old-account')")
	old.Close(ctx)
	if err != nil {
		t.Fatal(err)
	}
	database.Setup(config.DatabaseConfig{Host: cfg.Host, Port: uint(cfg.Port), Username: cfg.User, Password: cfg.Password, DB: name})
	db := database.GetClient()
	defer db.Close()
	snapshot := discovery.Result{Domains: []discovery.DomainInfo{{Domain: "test.example.com", Bucket: "cloud-bucket", Account: &ent.CloudAccount{Name: "cloud-account"}}, {Domain: "legacy.example.com", Account: &ent.CloudAccount{Name: "renamed-account"}}}}
	states, err := management.Load(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	local := states["test.example.com"]
	if local.AccountName != "" || local.Bucket != "" || local.Region != "" {
		t.Fatal("cloud metadata was copied to local management state")
	}
	if states["legacy.example.com"].AccountName != "old-account" {
		t.Fatal("compatibility columns were unexpectedly rewritten")
	}
	if err := local.Update().SetManaged(false).SetRetryCount(3).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	states, err = management.Load(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if states["test.example.com"].Managed || states["test.example.com"].RetryCount != 3 {
		t.Fatal("refresh lost management intent")
	}
	local = states["test.example.com"]
	cert, key := testMaterial(t, local.Domain, time.Now().Add(90*24*time.Hour))
	if err := saveCertificate(ctx, local, cert, key); err != nil {
		t.Fatal(err)
	}
	if err := local.Update().SetStatus("error").SetErrorMessage("simulated deployment failure").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	reloaded, err := db.Domain.Get(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Even a missing/wrong old DB expiry must not cause another ACME order.
	reloaded.ExpiresAt = nil
	gotCert, gotKey, _, issued, err := New(zap.NewNop()).ensureCert(ctx, reloaded, nil, nil, &ent.AppSettings{RenewBeforeDays: 30}, &taskLog{ctx: ctx})
	if err != nil || issued || gotCert != cert || gotKey != key {
		t.Fatalf("deployment retry did not reuse durable material: issued=%v err=%v", issued, err)
	}
	task, err := db.RenewTask.Create().SetDomain(local.Domain).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() { New(zap.NewNop()).executeTask(ctx, task, nil, nil, nil) })
	}
	group.Wait()
	after, err := db.Domain.Get(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.RetryCount != 4 {
		t.Fatalf("same task executed repeatedly: retry_count=%d", after.RetryCount)
	}
	finished, err := db.RenewTask.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != renewtask.StatusFailed {
		t.Fatalf("task not finalized: %v", finished.Status)
	}
}
