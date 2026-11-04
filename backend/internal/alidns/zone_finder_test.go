package alidns

import (
	"context"
	"net"
	"testing"

	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/miekg/dns"
)

func TestInspectZoneUsesConfiguredResolver(t *testing.T) {
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
			switch req.Question[0].Qtype {
			case dns.TypeSOA:
				resp.Answer = []dns.RR{&dns.SOA{
					Hdr: dns.RR_Header{Name: "example.invalid.", Rrtype: dns.TypeSOA, Class: dns.ClassINET},
					Ns:  "dns1.hichina.com.", Mbox: "hostmaster.example.invalid.", Serial: 1,
				}}
			case dns.TypeNS:
				resp.Answer = []dns.RR{&dns.NS{
					Hdr: dns.RR_Header{Name: "example.invalid.", Rrtype: dns.TypeNS, Class: dns.ClassINET},
					Ns:  "dns1.hichina.com.",
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
	original := dns01.DefaultClient()
	zone, aliyun, err := InspectZone(t.Context(), "host.example.invalid", []string{conn.LocalAddr().String()})
	if err != nil || zone != "example.invalid" || !aliyun {
		t.Fatalf("InspectZone() = %q, %v, %v", zone, aliyun, err)
	}
	if dns01.DefaultClient() != original {
		t.Fatal("zone inspection changed the global issuance DNS client")
	}
}

func TestInspectZoneCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := InspectZone(ctx, "host.example.invalid", []string{"127.0.0.1:1"}); err == nil {
		t.Fatal("expected canceled DNS lookup to fail")
	}
}
