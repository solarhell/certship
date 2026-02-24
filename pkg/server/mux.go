package server

import (
	"net/http"
	"net/http/pprof"

	"connectrpc.com/connect"

	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/logic/appsettings"
	"github.com/solarhell/certship/pkg/logic/auth"
	"github.com/solarhell/certship/pkg/logic/certificate"
	"github.com/solarhell/certship/pkg/logic/cloudaccount"
	"github.com/solarhell/certship/pkg/logic/notificationchannel"
	"github.com/solarhell/certship/pkg/server/middleware"
)

func Setup() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	authInterceptor := connect.WithInterceptors(middleware.NewAuthInterceptor())

	mux.Handle(certshipv1connect.NewAuthServiceHandler(auth.NewServer(), authInterceptor))
	mux.Handle(certshipv1connect.NewCertificateServiceHandler(certificate.NewServer(), authInterceptor))
	mux.Handle(certshipv1connect.NewCloudAccountServiceHandler(cloudaccount.NewServer(), authInterceptor))
	mux.Handle(certshipv1connect.NewAppSettingsServiceHandler(appsettings.NewServer(), authInterceptor))
	mux.Handle(certshipv1connect.NewNotificationChannelServiceHandler(notificationchannel.NewServer(), authInterceptor))

	return mux
}
