package acme

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/miekg/dns"
)

func TestResolverLeasesPreserveConcurrentConfiguration(t *testing.T) {
	original := dns01.DefaultClient()
	t.Cleanup(func() { dns01.SetDefaultClient(original) })
	first := []string{startSOAResolver(t, "first.invalid.")}
	second := []string{startSOAResolver(t, "second.invalid.")}
	var leases resolverLeases
	releaseFirst, err := leases.acquire(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	client := dns01.DefaultClient()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	releaseConcurrent, err := leases.acquire(ctx, first)
	if err != nil {
		releaseFirst()
		t.Fatalf("same resolver configuration must allow concurrent issuance: %v", err)
	}
	if dns01.DefaultClient() != client {
		t.Error("concurrent issuance replaced the active DNS client")
	}
	releaseFirst()
	waitCtx, stopWaiting := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stopWaiting()
	release, err := leases.acquire(waitCtx, second)
	if err == nil {
		release()
		t.Error("resolver switched while an issuance was still active")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("unexpected resolver wait error: %v", err)
	}
	if dns01.DefaultClient() != client {
		t.Error("canceled configuration switch changed the active DNS client")
	}
	if zone, err := client.FindZoneByFqdn(t.Context(), "host.first.invalid."); err != nil || zone != "first.invalid." {
		t.Errorf("active issuance no longer uses its original resolver: %q, %v", zone, err)
	}

	// A waiter for the new configuration must wake after the final old lease ends.
	type result struct {
		release func()
		err     error
	}
	ready := make(chan result, 1)
	go func() {
		release, err := leases.acquire(ctx, second)
		ready <- result{release, err}
	}()
	releaseConcurrent()
	r := <-ready
	if r.err != nil {
		t.Fatal(r.err)
	}
	defer r.release()
	if zone, err := dns01.DefaultClient().FindZoneByFqdn(t.Context(), "host.second.invalid."); err != nil || zone != "second.invalid." {
		t.Fatalf("new issuance did not use the new resolver: %q, %v", zone, err)
	}
}

func startSOAResolver(t *testing.T, zone string) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := &dns.Server{
		PacketConn:        conn,
		NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
			resp := new(dns.Msg).SetReply(req)
			if req.Question[0].Qtype == dns.TypeSOA {
				resp.Answer = []dns.RR{&dns.SOA{
					Hdr: dns.RR_Header{Name: zone, Rrtype: dns.TypeSOA, Class: dns.ClassINET},
					Ns:  "ns." + zone, Mbox: "hostmaster." + zone, Serial: 1,
				}}
			}
			_ = w.WriteMsg(resp)
		}),
	}
	go func() {
		if err := server.ActivateAndServe(); err != nil {
			t.Errorf("DNS server failed: %v", err)
		}
	}()
	<-started
	t.Cleanup(func() { _ = server.Shutdown() })
	return conn.LocalAddr().String()
}
