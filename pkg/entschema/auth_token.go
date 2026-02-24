package entschema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type AuthToken struct {
	ent.Schema
}

func (AuthToken) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_auth_token"),
		entsql.WithComments(true),
	}
}

func (AuthToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().DefaultFunc(uuid.NewString).Comment("主键 UUID"),

		field.String("admin_id").Immutable().Comment("管理员 ID"),
		field.String("token").Unique().Immutable().NotEmpty().Comment("Token"),

		field.String("login_user_agent").Immutable().Comment("登录时的 User-Agent"),
		field.String("login_ip").Immutable().Comment("登录时的 IP"),
		field.Time("login_time").Immutable().Comment("登录时间"),

		field.String("last_active_user_agent").Comment("最后活跃的 User-Agent"),
		field.String("last_active_ip").Comment("最后活跃的 IP"),
		field.Time("last_active_time").Comment("最后活跃时间"),
	}
}

func (AuthToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("admin_id"),
		index.Fields("token"),
		index.Fields("last_active_time"),
	}
}
