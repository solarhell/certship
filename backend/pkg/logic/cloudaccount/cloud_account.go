package cloudaccount

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/internal/domainsync"
	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

const rescanTimeout = 2 * time.Minute

func NewServer() certshipv1connect.CloudAccountServiceHandler {
	return &Server{logger: zap.L().Named("cloud_account")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) ListCloudAccounts(ctx context.Context, _ *certshipv1.ListCloudAccountsRequest) (*certshipv1.ListCloudAccountsResponse, error) {
	accounts, err := database.GetClient().CloudAccount.Query().All(ctx)
	if err != nil {
		s.logger.Error("查询云账号列表失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	items := make([]*certshipv1.CloudAccountItem, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, &certshipv1.CloudAccountItem{
			Id:          a.ID,
			Name:        a.Name,
			AccessKeyId: a.AccessKeyID,
			Enabled:     a.Enabled,
			CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return &certshipv1.ListCloudAccountsResponse{Accounts: items}, nil
}

func (s *Server) CreateCloudAccount(ctx context.Context, req *certshipv1.CreateCloudAccountRequest) (*certshipv1.CreateCloudAccountResponse, error) {
	if req.Name == "" || req.AccessKeyId == "" || req.AccessKeySecret == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "name、access_key_id、access_key_secret 不能为空")
	}

	a, err := database.GetClient().CloudAccount.Create().
		SetName(req.Name).
		SetAccessKeyID(req.AccessKeyId).
		SetAccessKeySecret(req.AccessKeySecret).
		SetEnabled(req.Enabled).
		Save(ctx)
	if err != nil {
		s.logger.Error("创建云账号失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	return &certshipv1.CreateCloudAccountResponse{Id: a.ID}, nil
}

func (s *Server) UpdateCloudAccount(ctx context.Context, req *certshipv1.UpdateCloudAccountRequest) (*certshipv1.UpdateCloudAccountResponse, error) {
	if req.Id == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "id 不能为空")
	}

	updater := database.GetClient().CloudAccount.UpdateOneID(req.Id)
	if req.Name != "" {
		updater = updater.SetName(req.Name)
	}
	if req.AccessKeyId != "" {
		updater = updater.SetAccessKeyID(req.AccessKeyId)
	}
	if req.AccessKeySecret != "" {
		updater = updater.SetAccessKeySecret(req.AccessKeySecret)
	}
	updater = updater.SetEnabled(req.Enabled)

	if err := updater.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.Errorf(connect.CodeNotFound, "云账号不存在")
		}
		s.logger.Error("更新云账号失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	return &certshipv1.UpdateCloudAccountResponse{}, nil
}

func (s *Server) RescanCloudAccount(ctx context.Context, req *certshipv1.RescanCloudAccountRequest) (*certshipv1.RescanCloudAccountResponse, error) {
	if req.Id == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "id 不能为空")
	}

	db := database.GetClient()
	account, err := db.CloudAccount.Get(ctx, req.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.Errorf(connect.CodeNotFound, "云账号不存在")
		}
		s.logger.Error("查询云账号失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}
	if !account.Enabled {
		return nil, connect.Errorf(connect.CodeFailedPrecondition, "云账号已禁用")
	}

	scanCtx, cancel := context.WithTimeout(ctx, rescanTimeout)
	defer cancel()

	// 单账号扫描:Coverage 只覆盖这个账号,对账因此也只会影响它自己的域名
	result := discovery.Run(scanCtx, s.logger, []*ent.CloudAccount{account})
	if result.Coverage.IsEmpty() {
		return nil, connect.Errorf(connect.CodeInternal, "扫描失败:该账号的 OSS 与 CDN 均未扫通,详见服务端日志")
	}

	missingGrace, archiveAfter := database.PresenceWindows(ctx)
	stats := domainsync.Reconcile(scanCtx, s.logger, result, missingGrace, archiveAfter)

	total, err := db.Domain.Query().Where(entdomain.AccountNameEQ(account.Name)).Count(ctx)
	if err != nil {
		s.logger.Warn("查询账号域名总数失败", zap.Error(err))
	}

	return &certshipv1.RescanCloudAccountResponse{
		Added:    uint32(stats.Added),
		Updated:  uint32(stats.Updated),
		Total:    uint32(total),
		Missing:  uint32(len(stats.Missing)),
		Archived: uint32(len(stats.Archived)),
		Revived:  uint32(len(stats.Revived)),
	}, nil
}

func (s *Server) DeleteCloudAccount(ctx context.Context, req *certshipv1.DeleteCloudAccountRequest) (*certshipv1.DeleteCloudAccountResponse, error) {
	if req.Id == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "id 不能为空")
	}

	err := database.GetClient().CloudAccount.
		DeleteOneID(req.Id).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.Errorf(connect.CodeNotFound, "云账号不存在")
		}
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	return &certshipv1.DeleteCloudAccountResponse{}, nil
}
