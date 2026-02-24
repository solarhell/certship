package entschema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type Admin struct {
	ent.Schema
}

func (Admin) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_admin"),
		entsql.WithComments(true),
	}
}

func (Admin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().DefaultFunc(uuid.NewString).Comment("主键 UUID"),

		field.String("username").Unique().Immutable().NotEmpty().Comment("管理员用户名"),
		field.String("password_hash").NotEmpty().Comment("密码 bcrypt hash"),

		field.Time("created_at").Immutable().Default(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("创建时间"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("更新时间"),
	}
}

func (Admin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("username"),
	}
}
