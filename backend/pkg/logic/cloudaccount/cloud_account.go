package cloudaccount

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/domainsync"
	"github.com/solarhell/certship/internal/oss"
	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entdomain "github.com/solarhell/certship/pkg/ent/domain"
)

func NewServer() certshipv1connect.CloudAccountServiceHandler {
	return &Server{logger: zap.L().Named("cloud_account")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) ListCloudAccounts(ctx context.Context, _ *connect.Request[certshipv1.ListCloudAccountsRequest]) (*connect.Response[certshipv1.ListCloudAccountsResponse], error) {
	accounts, err := database.GetClient().CloudAccount.Query().All(ctx)
	if err != nil {
		s.logger.Error("查询云账号列表失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
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

	return connect.NewResponse(&certshipv1.ListCloudAccountsResponse{Accounts: items}), nil
}

func (s *Server) CreateCloudAccount(ctx context.Context, req *connect.Request[certshipv1.CreateCloudAccountRequest]) (*connect.Response[certshipv1.CreateCloudAccountResponse], error) {
	if req.Msg.Name == "" || req.Msg.AccessKeyId == "" || req.Msg.AccessKeySecret == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name、access_key_id、access_key_secret 不能为空"))
	}

	a, err := database.GetClient().CloudAccount.Create().
		SetName(req.Msg.Name).
		SetAccessKeyID(req.Msg.AccessKeyId).
		SetAccessKeySecret(req.Msg.AccessKeySecret).
		SetEnabled(req.Msg.Enabled).
		Save(ctx)
	if err != nil {
		s.logger.Error("创建云账号失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.CreateCloudAccountResponse{Id: a.ID}), nil
}

func (s *Server) UpdateCloudAccount(ctx context.Context, req *connect.Request[certshipv1.UpdateCloudAccountRequest]) (*connect.Response[certshipv1.UpdateCloudAccountResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	updater := database.GetClient().CloudAccount.UpdateOneID(req.Msg.Id)
	if req.Msg.Name != "" {
		updater = updater.SetName(req.Msg.Name)
	}
	if req.Msg.AccessKeyId != "" {
		updater = updater.SetAccessKeyID(req.Msg.AccessKeyId)
	}
	if req.Msg.AccessKeySecret != "" {
		updater = updater.SetAccessKeySecret(req.Msg.AccessKeySecret)
	}
	updater = updater.SetEnabled(req.Msg.Enabled)

	if err := updater.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("云账号不存在"))
		}
		s.logger.Error("更新云账号失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.UpdateCloudAccountResponse{}), nil
}

func (s *Server) RescanCloudAccount(ctx context.Context, req *connect.Request[certshipv1.RescanCloudAccountRequest]) (*connect.Response[certshipv1.RescanCloudAccountResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	db := database.GetClient()
	account, err := db.CloudAccount.Get(ctx, req.Msg.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("云账号不存在"))
		}
		s.logger.Error("查询云账号失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !account.Enabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("云账号已禁用"))
	}

	scanner := oss.NewScanner(s.logger)
	domains, err := scanner.ScanAccount(ctx, account)
	if err != nil {
		s.logger.Error("扫描云账号失败", zap.String("account", account.Name), zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("扫描失败: %w", err))
	}

	stats := domainsync.Sync(ctx, s.logger, domains)

	total, err := db.Domain.Query().Where(entdomain.AccountNameEQ(account.Name)).Count(ctx)
	if err != nil {
		s.logger.Warn("查询账号域名总数失败", zap.Error(err))
	}

	return connect.NewResponse(&certshipv1.RescanCloudAccountResponse{
		Added:   uint32(stats.Added),
		Updated: uint32(stats.Updated),
		Total:   uint32(total),
	}), nil
}

func (s *Server) DeleteCloudAccount(ctx context.Context, req *connect.Request[certshipv1.DeleteCloudAccountRequest]) (*connect.Response[certshipv1.DeleteCloudAccountResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	err := database.GetClient().CloudAccount.
		DeleteOneID(req.Msg.Id).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("云账号不存在"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.DeleteCloudAccountResponse{}), nil
}
