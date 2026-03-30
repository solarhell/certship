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

// CloudAccount 阿里云账号（AK/SK）
type CloudAccount struct {
	ent.Schema
}

func (CloudAccount) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_cloud_account"),
		entsql.WithComments(true),
	}
}

func (CloudAccount) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().DefaultFunc(uuid.NewString).Comment("主键 UUID"),

		field.String("name").NotEmpty().Comment("账号名称"),
		field.String("access_key_id").NotEmpty().Comment("阿里云 Access Key ID"),
		field.String("access_key_secret").NotEmpty().Comment("阿里云 Access Key Secret"),
		field.Bool("enabled").Default(true).Comment("是否启用"),

		field.Time("created_at").Immutable().Default(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("创建时间"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("更新时间"),
	}
}

func (CloudAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}
