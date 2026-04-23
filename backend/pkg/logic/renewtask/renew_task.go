package renewtask

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
	"github.com/solarhell/certship/pkg/ent/renewtask"
	"github.com/solarhell/certship/pkg/model"
)

// TaskExecutor 任务执行接口
type TaskExecutor interface {
	ExecuteTaskByID(ctx context.Context, taskID string) error
}

func NewServer(executor TaskExecutor) certshipv1connect.RenewTaskServiceHandler {
	return &Server{logger: zap.L().Named("renewtask"), executor: executor}
}

type Server struct {
	logger   *zap.Logger
	executor TaskExecutor
}

func (s *Server) ListRenewTasks(ctx context.Context, req *connect.Request[certshipv1.ListRenewTasksRequest]) (*connect.Response[certshipv1.ListRenewTasksResponse], error) {
	db := database.GetClient()
	limit := model.NormalizeLimit(req.Msg.Limit)

	totalCount, err := db.RenewTask.Query().Count(ctx)
	if err != nil {
		s.logger.Error("查询任务总数失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// 按创建时间降序（最新的排前面）
	query := db.RenewTask.Query().
		Order(renewtask.ByCreatedAt(sql.OrderDesc())).
		Limit(int(limit + 1))

	if req.Msg.Cursor != nil && *req.Msg.Cursor != "" {
		cursorID, err := model.DecodeCursor(*req.Msg.Cursor)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("无效的分页游标"))
		}
		lastRecord, err := db.RenewTask.Get(ctx, cursorID)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("游标无效：记录不存在"))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		// keyset: (created_at DESC, id DESC)
		query = query.Where(
			renewtask.Or(
				renewtask.CreatedAtLT(lastRecord.CreatedAt),
				renewtask.And(
					renewtask.CreatedAtEQ(lastRecord.CreatedAt),
					renewtask.IDLT(cursorID),
				),
			),
		)
	}

	tasks, err := query.All(ctx)
	if err != nil {
		s.logger.Error("查询续期任务失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	hasMore := len(tasks) > int(limit)
	if hasMore {
		tasks = tasks[:limit]
	}

	items := make([]*certshipv1.RenewTaskItem, 0, len(tasks))
	for _, t := range tasks {
		item := &certshipv1.RenewTaskItem{
			Id:           t.ID,
			Domains:      t.Domains,
			Status:       string(t.Status),
			Trigger:      string(t.Trigger),
			ErrorMessage: t.ErrorMessage,
			CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		for _, entry := range t.Log {
			item.Log = append(item.Log, &certshipv1.TaskLogEntry{
				Time:    entry.Time.Format("2006-01-02T15:04:05Z07:00"),
				Content: entry.Content,
			})
		}
		if t.StartedAt != nil {
			item.StartedAt = t.StartedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		if t.FinishedAt != nil {
			item.FinishedAt = t.FinishedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		items = append(items, item)
	}

	resp := &certshipv1.ListRenewTasksResponse{
		Tasks:      items,
		HasMore:    hasMore,
		TotalCount: uint64(totalCount),
	}
	if hasMore && len(tasks) > 0 {
		nc := model.EncodeCursor(tasks[len(tasks)-1].ID)
		resp.NextCursor = &nc
	}

	return connect.NewResponse(resp), nil
}

func (s *Server) CreateRenewTask(ctx context.Context, req *connect.Request[certshipv1.CreateRenewTaskRequest]) (*connect.Response[certshipv1.CreateRenewTaskResponse], error) {
	domains := req.Msg.Domains
	if len(domains) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	db := database.GetClient()
	existingTasks, err := db.RenewTask.Query().
		Where(renewtask.StatusIn(renewtask.StatusPending, renewtask.StatusRunning)).
		All(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	busyDomains := make(map[string]bool)
	for _, t := range existingTasks {
		for _, dn := range t.Domains {
			busyDomains[dn] = true
		}
	}
	for _, d := range domains {
		if busyDomains[d] {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("域名 %s 已有进行中的续期任务", d))
		}
	}

	task, err := db.RenewTask.Create().
		SetDomains(domains).
		SetTrigger(renewtask.TriggerManual).
		Save(ctx)
	if err != nil {
		s.logger.Error("创建续期任务失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	go func() {
		if err := s.executor.ExecuteTaskByID(context.Background(), task.ID); err != nil {
			s.logger.Error("异步执行任务失败", zap.String("task_id", task.ID), zap.Error(err))
		}
	}()

	return connect.NewResponse(&certshipv1.CreateRenewTaskResponse{Id: task.ID}), nil
}
