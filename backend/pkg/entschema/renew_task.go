package entschema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/solarhell/certship/pkg/model"
)

// RenewTask 证书续期任务，一个任务只对应一个域名
type RenewTask struct {
	ent.Schema
}

func (RenewTask) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_renew_task"),
		entsql.WithComments(true),
	}
}

func (RenewTask) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().DefaultFunc(uuid.NewString).Comment("主键 UUID"),

		field.String("domain").NotEmpty().Comment("关联的域名"),

		field.Enum("status").
			Values("pending", "running", "success", "failed").
			Default("pending").
			Comment("任务状态：pending=待执行，running=执行中，success=成功，failed=失败"),

		field.Enum("trigger").
			Values("auto", "manual").
			Default("auto").
			Comment("触发方式：auto=自动，manual=手动"),

		field.JSON("log", []model.TaskLogEntry{}).
			Optional().
			Comment("执行日志"),

		field.Text("error_message").
			Optional().
			Comment("失败原因"),

		field.Time("started_at").Optional().Nillable().Comment("开始执行时间"),
		field.Time("finished_at").Optional().Nillable().Comment("完成时间"),

		field.Time("created_at").Immutable().Default(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("创建时间"),
	}
}

func (RenewTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("created_at"),
	}
}
