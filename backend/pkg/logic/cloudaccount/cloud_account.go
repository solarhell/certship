package cloudaccount

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
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
