package server

import (
	"context"
	"net/http"
	"net/http/pprof"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/logic/appsettings"
	"github.com/solarhell/certship/pkg/logic/auth"
	"github.com/solarhell/certship/pkg/logic/certificate"
	"github.com/solarhell/certship/pkg/logic/cloudaccount"
	"github.com/solarhell/certship/pkg/logic/notificationchannel"
	"github.com/solarhell/certship/pkg/logic/renewtask"
	"github.com/solarhell/certship/pkg/server/middleware"
)

// TaskExecutor 供 API 层调用的任务执行接口
type TaskExecutor interface {
	ExecuteTaskByID(ctx context.Context, taskID string) error
}

func Setup(executor TaskExecutor) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := connect.NewServer(middleware.NewAuthInterceptor())
	certshipv1connect.RegisterAuthServiceHandler(server, auth.NewServer())
	certshipv1connect.RegisterCertificateServiceHandler(server, certificate.NewServer())
	certshipv1connect.RegisterCloudAccountServiceHandler(server, cloudaccount.NewServer())
	certshipv1connect.RegisterAppSettingsServiceHandler(server, appsettings.NewServer())
	certshipv1connect.RegisterNotificationChannelServiceHandler(server, notificationchannel.NewServer())
	certshipv1connect.RegisterRenewTaskServiceHandler(server, renewtask.NewServer(executor))
	connecthttp.Mount(mux, server)

	return mux
}
