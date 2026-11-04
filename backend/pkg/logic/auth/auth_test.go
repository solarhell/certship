package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
)

// Use the same metadata reader as Login without requiring a database or issuing real tokens.
type loginSourceServer struct {
	certshipv1connect.UnimplementedAuthServiceHandler
}

func (loginSourceServer) Login(ctx context.Context, _ *certshipv1.LoginRequest) (*certshipv1.LoginResponse, error) {
	ua, ip := loginSource(ctx)
	return &certshipv1.LoginResponse{Token: ua + "|" + ip}, nil
}

func TestLoginSourceFromV2CallContext(t *testing.T) {
	rpcServer := connect.NewServer()
	certshipv1connect.RegisterAuthServiceHandler(rpcServer, loginSourceServer{})
	handler := http.NewServeMux()
	connecthttp.Mount(handler, rpcServer)
	mux := httptest.NewServer(handler)
	defer mux.Close()
	for _, transport := range []struct {
		name      string
		transport connect.Transport
	}{
		{"HTTP", connecthttp.NewTransport(mux.Client(), mux.URL)},
		{"in process", connectinprocess.New(rpcServer)},
	} {
		for _, tc := range []struct {
			name, realIP, forwarded, wantIP string
		}{
			{"real IP preferred", "192.0.2.1", "198.51.100.2", "192.0.2.1"},
			{"forwarded fallback", "", "198.51.100.2, 192.0.2.2", "198.51.100.2, 192.0.2.2"},
			{"no IP header", "", "", ""},
		} {
			t.Run(transport.name+"/"+tc.name, func(t *testing.T) {
				ctx, info := connect.NewClientContext(t.Context())
				info.RequestHeader().Set("User-Agent", "CertShip migration test")
				info.RequestHeader().Set("X-Real-IP", tc.realIP)
				info.RequestHeader().Set("X-Forwarded-For", tc.forwarded)
				client := certshipv1connect.NewAuthServiceClient(connect.NewClient(transport.transport))
				resp, err := client.Login(ctx, &certshipv1.LoginRequest{})
				if err != nil {
					t.Fatal(err)
				}
				if want := "CertShip migration test|" + tc.wantIP; resp.Token != want {
					t.Fatalf("login metadata = %q, want %q", resp.Token, want)
				}
			})
		}
	}
}

func TestLoginSourceWithoutCallContext(t *testing.T) {
	if ua, ip := loginSource(t.Context()); ua != "" || ip != "" {
		t.Fatalf("unexpected metadata without RPC context: %q %q", ua, ip)
	}
}
