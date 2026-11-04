package certificate

import (
	"context"
	"sort"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/solarhell/certship/internal/cloudstate"
	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/internal/management"
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

func (s *Server) ListCertificates(ctx context.Context, req *certshipv1.ListCertificatesRequest) (*certshipv1.ListCertificatesResponse, error) {
	snapshot, err := cloudstate.Refresh(ctx, s.logger, true)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, err.Error()).WithCause(err)
	}
	if !snapshot.Complete() {
		return nil, connect.Errorf(connect.CodeUnavailable, "阿里云资源或证书查询不完整，请稍后重试；本地记录不作为当前资源列表")
	}
	states, err := management.Load(ctx, snapshot)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	items := currentCertificates(snapshot, states)
	start := 0
	if req.Cursor != nil && *req.Cursor != "" {
		id, err := model.DecodeCursor(*req.Cursor)
		if err != nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "无效的分页游标")
		}
		found := false
		for i, item := range items {
			if item.Id == id {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "云资源列表已变化，请从第一页刷新")
		}
	}
	limit := int(model.NormalizeLimit(req.Limit))
	end := min(start+limit, len(items))
	response := &certshipv1.ListCertificatesResponse{Certificates: items[start:end], TotalCount: uint64(len(items)), HasMore: end < len(items)}
	if response.HasMore {
		next := model.EncodeCursor(items[end-1].Id)
		response.NextCursor = &next
	}
	return response, nil
}

// Local state only supplies IDs, intent, retry state and errors. Resource location
// and online certificate validity always come from this cloud observation.
func currentCertificates(snapshot discovery.Result, states map[string]*ent.Domain) []*certshipv1.CertificateItem {
	infos := append([]discovery.DomainInfo(nil), snapshot.Domains...)
	sort.Slice(infos, func(i, j int) bool {
		a, b := infos[i].CloudCert(), infos[j].CloudCert()
		if a == nil && b != nil {
			return true
		}
		if a != nil && b == nil {
			return false
		}
		if a != nil && b != nil && !a.ValidEndDate.Equal(b.ValidEndDate) {
			return a.ValidEndDate.Before(b.ValidEndDate)
		}
		return states[infos[i].Domain].ID < states[infos[j].Domain].ID
	})
	items := make([]*certshipv1.CertificateItem, 0, len(infos))
	for _, info := range infos {
		state := states[info.Domain]
		item := &certshipv1.CertificateItem{Id: state.ID, Domain: info.Domain, Bucket: info.Bucket, Region: info.Region, AccountName: info.Account.Name, DeployTarget: string(info.DeployTarget()), Origin: string(info.Origin), Managed: state.Managed, RetryCount: uint32(state.RetryCount), ErrorKind: string(state.ErrorKind), ErrorMessage: state.ErrorMessage, BlockedReason: state.BlockedReason, NextRetryAt: formatTime(state.NextRetryAt), Status: "pending"}
		if cert := info.CloudCert(); cert != nil {
			item.IssuedAt = cert.ValidStartDate.UTC().Format(time.RFC3339)
			item.ExpiresAt = cert.ValidEndDate.UTC().Format(time.RFC3339)
			item.Status = "active"
			if !cert.ValidEndDate.After(time.Now()) {
				item.Status = "error"
			}
		}
		items = append(items, item)
	}
	return items
}

// SetCertificateManaged 暂停或恢复 certship 对该域名的托管。
//
// 这是"我不想让 certship 管这个域名"的正确表达方式——记录、历史和证书都留着,
// 只是不再自动签发续期。恢复托管时顺带清掉阻塞与退避,让它能立刻重新开始。
func (s *Server) SetCertificateManaged(ctx context.Context, req *certshipv1.SetCertificateManagedRequest) (*certshipv1.SetCertificateManagedResponse, error) {
	if req.Id == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "id 不能为空")
	}

	db := database.GetClient()
	record, err := db.Domain.Get(ctx, req.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.Errorf(connect.CodeNotFound, "域名记录不存在")
		}
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	updater := record.Update().SetManaged(req.Managed)
	if req.Managed {
		updater = updater.
			SetBlockedReason("").
			SetErrorKind(domain.ErrorKindNone).
			SetRetryCount(0).
			ClearNextRetryAt()
	}
	if err := updater.Exec(ctx); err != nil {
		s.logger.Error("更新托管状态失败", zap.String("domain", record.Domain), zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	s.logger.Info("域名托管状态已变更",
		zap.String("domain", record.Domain),
		zap.Bool("managed", req.Managed),
	)
	return &certshipv1.SetCertificateManagedResponse{}, nil
}

// formatTime 把可空时间格式化为 RFC3339,空值返回空串
func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
