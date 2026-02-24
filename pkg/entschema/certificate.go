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

type Certificate struct {
	ent.Schema
}

func (Certificate) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_certificate"),
		entsql.WithComments(true),
	}
}

func (Certificate) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().DefaultFunc(uuid.NewString).Comment("主键 UUID"),

		field.String("domain").NotEmpty().Comment("OSS 自定义域名"),
		field.String("bucket").NotEmpty().Comment("OSS bucket 名称"),
		field.String("region").NotEmpty().Comment("OSS 区域，如 cn-hangzhou"),
		field.String("account_name").NotEmpty().Comment("阿里云账号名称"),

		field.Time("issued_at").Optional().Nillable().Comment("证书颁发时间"),
		field.Time("expires_at").Optional().Nillable().Comment("证书过期时间"),

		field.Text("cert_pem").Optional().Comment("证书 PEM 内容"),
		field.Text("key_pem").Optional().Comment("证书私钥 PEM 内容"),

		field.Enum("status").
			Values("pending", "active", "error").
			Default("pending").
			Comment("证书状态：pending=待颁发，active=有效，error=错误"),

		field.Text("error_message").
			Optional().
			Comment("最近一次错误信息"),

		field.Time("created_at").Immutable().Default(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("创建时间"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("更新时间"),
	}
}

func (Certificate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("domain").Unique(),
		index.Fields("expires_at"),
		index.Fields("status"),
	}
}
