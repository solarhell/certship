package certificate

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"entgo.io/ent/dialect/sql"
	"go.uber.org/zap"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	"github.com/solarhell/certship/pkg/ent/domain"
	"github.com/solarhell/certship/pkg/model"
)

func NewServer() certshipv1connect.CertificateServiceHandler {
	return &Server{logger: zap.L().Named("certificate")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) ListCertificates(ctx context.Context, req *connect.Request[certshipv1.ListCertificatesRequest]) (*connect.Response[certshipv1.ListCertificatesResponse], error) {
	db := database.GetClient()
	limit := model.NormalizeLimit(req.Msg.Limit)

	// 总数
	totalCount, err := db.Domain.Query().Count(ctx)
	if err != nil {
		s.logger.Error("查询证书总数失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// 按过期时间升序（快过期的排前面），NULL 排最前（pending 状态无过期时间）
	query := db.Domain.Query().
		Order(domain.ByExpiresAt(sql.OrderNullsFirst())).
		Limit(int(limit + 1))

	// 应用 cursor
	if req.Msg.Cursor != nil && *req.Msg.Cursor != "" {
		cursorID, err := model.DecodeCursor(*req.Msg.Cursor)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("无效的分页游标"))
		}
		lastRecord, err := db.Domain.Get(ctx, cursorID)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("游标无效：记录不存在"))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		// keyset: (expires_at ASC, id ASC) with NULL first
		if lastRecord.ExpiresAt != nil {
			query = query.Where(
				domain.Or(
					domain.ExpiresAtIsNil(),
					domain.ExpiresAtGT(*lastRecord.ExpiresAt),
					domain.And(
						domain.ExpiresAtEQ(*lastRecord.ExpiresAt),
						domain.IDGT(cursorID),
					),
				),
			)
		} else {
			query = query.Where(
				domain.Or(
					domain.And(
						domain.ExpiresAtIsNil(),
						domain.IDGT(cursorID),
					),
					domain.ExpiresAtNotNil(),
				),
			)
		}
	}

	certs, err := query.All(ctx)
	if err != nil {
		s.logger.Error("查询证书列表失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	hasMore := len(certs) > int(limit)
	if hasMore {
		certs = certs[:limit]
	}

	items := make([]*certshipv1.CertificateItem, 0, len(certs))
	for _, c := range certs {
		item := &certshipv1.CertificateItem{
			Id:            c.ID,
			Domain:        c.Domain,
			Bucket:        c.Bucket,
			Region:        c.Region,
			AccountName:   c.AccountName,
			Status:        string(c.Status),
			ErrorMessage:  c.ErrorMessage,
			DeployTarget:  string(c.DeployTarget),
			BlockedReason: c.BlockedReason,
		}
		if c.IssuedAt != nil {
			v := c.IssuedAt.Format("2006-01-02T15:04:05Z07:00")
			item.IssuedAt = v
		}
		if c.ExpiresAt != nil {
			v := c.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
			item.ExpiresAt = v
		}
		items = append(items, item)
	}

	resp := &certshipv1.ListCertificatesResponse{
		Certificates: items,
		HasMore:      hasMore,
		TotalCount:   uint64(totalCount),
	}
	if hasMore && len(certs) > 0 {
		nc := model.EncodeCursor(certs[len(certs)-1].ID)
		resp.NextCursor = &nc
	}

	return connect.NewResponse(resp), nil
}
