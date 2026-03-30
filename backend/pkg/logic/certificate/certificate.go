package certificate

import (
	"context"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
)

func NewServer() certshipv1connect.CertificateServiceHandler {
	return &Server{logger: zap.L().Named("certificate")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) ListCertificates(ctx context.Context, _ *connect.Request[certshipv1.ListCertificatesRequest]) (*connect.Response[certshipv1.ListCertificatesResponse], error) {
	certs, err := database.GetClient().Domain.Query().All(ctx)
	if err != nil {
		s.logger.Error("查询证书列表失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	items := make([]*certshipv1.CertificateItem, 0, len(certs))
	for _, c := range certs {
		items = append(items, &certshipv1.CertificateItem{
			Id:           c.ID,
			Domain:       c.Domain,
			Bucket:       c.Bucket,
			Region:       c.Region,
			AccountName:  c.AccountName,
			Status:       string(c.Status),
			IssuedAt:     c.IssuedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExpiresAt:    c.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			ErrorMessage: c.ErrorMessage,
		})
	}

	return connect.NewResponse(&certshipv1.ListCertificatesResponse{Certificates: items}), nil
}
