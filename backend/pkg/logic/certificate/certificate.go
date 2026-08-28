package certificate

import (
	"context"
	"fmt"
	"time"

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
			Presence:      string(c.Presence),
			Origin:        string(c.Origin),
			Managed:       c.Managed,
			RetryCount:    uint32(c.RetryCount),
			ErrorKind:     string(c.ErrorKind),
		}
		item.IssuedAt = formatTime(c.IssuedAt)
		item.ExpiresAt = formatTime(c.ExpiresAt)
		item.LastSeenAt = formatTime(c.LastSeenAt)
		item.NextRetryAt = formatTime(c.NextRetryAt)
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

// SetCertificateManaged 暂停或恢复 certship 对该域名的托管。
//
// 这是"我不想让 certship 管这个域名"的正确表达方式——记录、历史和证书都留着,
// 只是不再自动签发续期。恢复托管时顺带清掉阻塞与退避,让它能立刻重新开始。
func (s *Server) SetCertificateManaged(ctx context.Context, req *connect.Request[certshipv1.SetCertificateManagedRequest]) (*connect.Response[certshipv1.SetCertificateManagedResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	db := database.GetClient()
	record, err := db.Domain.Get(ctx, req.Msg.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("域名记录不存在"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	updater := record.Update().SetManaged(req.Msg.Managed)
	if req.Msg.Managed {
		updater = updater.
			SetBlockedReason("").
			SetErrorKind(domain.ErrorKindNone).
			SetRetryCount(0).
			ClearNextRetryAt()
	}
	if err := updater.Exec(ctx); err != nil {
		s.logger.Error("更新托管状态失败", zap.String("domain", record.Domain), zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.logger.Info("域名托管状态已变更",
		zap.String("domain", record.Domain),
		zap.Bool("managed", req.Msg.Managed),
	)
	return connect.NewResponse(&certshipv1.SetCertificateManagedResponse{}), nil
}

// DeleteCertificate 删除一条已归档的域名记录。
//
// 只对 archived 开放是有意为之:数据库是云上现状的投影,
// 云上还在的域名删掉也会被下一轮扫描原样写回来,徒增困惑。
func (s *Server) DeleteCertificate(ctx context.Context, req *connect.Request[certshipv1.DeleteCertificateRequest]) (*connect.Response[certshipv1.DeleteCertificateResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	db := database.GetClient()
	record, err := db.Domain.Get(ctx, req.Msg.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("域名记录不存在"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if record.Presence != domain.PresenceArchived {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(
			"域名 %s 在云上仍然存在(%s),删除后会被下一轮扫描重新写入；如果只是不想让 certship 管它,请改用暂停托管",
			record.Domain, record.Presence,
		))
	}

	if err := db.Domain.DeleteOne(record).Exec(ctx); err != nil {
		s.logger.Error("删除域名记录失败", zap.String("domain", record.Domain), zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.logger.Info("已删除归档的域名记录", zap.String("domain", record.Domain))
	return connect.NewResponse(&certshipv1.DeleteCertificateResponse{}), nil
}

// formatTime 把可空时间格式化为 RFC3339,空值返回空串
func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
