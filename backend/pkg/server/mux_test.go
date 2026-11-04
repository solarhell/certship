package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/reflect/protoreflect"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/server"
	"github.com/solarhell/certship/pkg/server/middleware"

	json "encoding/json/v2"
)

func TestEveryRPCIsMountedAndProtected(t *testing.T) {
	handler := server.Setup(nil)
	files := []protoreflect.FileDescriptor{
		certshipv1.File_certship_v1_auth_proto,
		certshipv1.File_certship_v1_app_settings_proto,
		certshipv1.File_certship_v1_certificate_proto,
		certshipv1.File_certship_v1_cloud_account_proto,
		certshipv1.File_certship_v1_notification_channel_proto,
		certshipv1.File_certship_v1_renew_task_proto,
	}
	for _, file := range files {
		services := file.Services()
		for i := range services.Len() {
			svc := services.Get(i)
			for j := range svc.Methods().Len() {
				procedure := "/" + string(svc.FullName()) + "/" + string(svc.Methods().Get(j).Name())
				t.Run(procedure, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(`{}`))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Connect-Protocol-Version", "1")
					resp := httptest.NewRecorder()
					handler.ServeHTTP(resp, req)
					if procedure == certshipv1connect.AuthServiceLoginProcedure {
						assertWireError(t, resp, http.StatusBadRequest, "invalid_argument", "用户名和密码不能为空")
					} else {
						assertWireError(t, resp, http.StatusUnauthorized, "unauthenticated", "token 不能为空")
					}
				})
			}
		}
	}
}

func TestGeneratedClientsAcrossHTTPProtocols(t *testing.T) {
	httpServer := httptest.NewServer(server.Setup(nil))
	defer httpServer.Close()
	for _, tc := range []struct {
		name string
		opts []connecthttp.Option
	}{
		{"Connect protobuf", nil},
		{"Connect JSON", []connecthttp.Option{connecthttp.WithProtoJSON()}},
		{"gRPC", []connecthttp.Option{connecthttp.WithGRPC()}},
		{"gRPC-Web", []connecthttp.Option{connecthttp.WithGRPCWeb()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL, tc.opts...))
			_, err := certshipv1connect.NewAuthServiceClient(client).Login(t.Context(), &certshipv1.LoginRequest{})
			assertRPCError(t, err, connect.CodeInvalidArgument, "用户名和密码不能为空")
			_, err = certshipv1connect.NewAppSettingsServiceClient(client).GetAppSettings(t.Context(), &certshipv1.GetAppSettingsRequest{})
			assertRPCError(t, err, connect.CodeUnauthenticated, "token 不能为空")
		})
	}
}

func TestLoginEnforcesDefaultRequestLimit(t *testing.T) {
	httpServer := httptest.NewServer(server.Setup(nil))
	defer httpServer.Close()
	client := connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL))
	_, err := certshipv1connect.NewAuthServiceClient(client).Login(t.Context(), &certshipv1.LoginRequest{Username: strings.Repeat("x", 4*1024*1024+1)})
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("oversized request was not rejected: %v", err)
	}
}

func TestCORSPreflightAllowsBrowserAuthentication(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, certshipv1connect.AppSettingsServiceGetAppSettingsProcedure, nil)
	req.Header.Set("Origin", "https://certship.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type,connect-protocol-version")
	resp := httptest.NewRecorder()
	middleware.CORS().Handler(server.Setup(nil)).ServeHTTP(resp, req)
	if resp.Code != http.StatusNoContent || resp.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight was rejected: %d %v", resp.Code, resp.Header())
	}
	allowed := strings.ToLower(resp.Header().Get("Access-Control-Allow-Headers"))
	for _, header := range []string{"authorization", "content-type", "connect-protocol-version"} {
		if !strings.Contains(allowed, header) {
			t.Errorf("preflight did not allow %s: %s", header, allowed)
		}
	}
}

func assertWireError(t *testing.T, resp *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid error response: %d %s: %v", resp.Code, body, err)
	}
	if resp.Code != status || payload.Code != code || payload.Message != message {
		t.Fatalf("got %d %s, want %d %s %q", resp.Code, body, status, code, message)
	}
}

func assertRPCError(t *testing.T, err error, code connect.Code, message string) {
	t.Helper()
	if connect.CodeOf(err) != code || !strings.Contains(err.Error(), message) {
		t.Fatalf("got %v, want %s %q", err, code, message)
	}
}
