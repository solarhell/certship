package entschema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// AppSettings 全局单行配置表
type AppSettings struct {
	ent.Schema
}

func (AppSettings) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_app_settings"),
		entsql.WithComments(true),
	}
}

func (AppSettings) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().Comment("固定为 default，单行配置"),

		field.String("acme_email").NotEmpty().Comment("Let's Encrypt 注册邮箱"),
		field.String("acme_directory_url").NotEmpty().Default("https://acme-v02.api.letsencrypt.org/directory").Comment("ACME 目录 URL"),
		field.Text("acme_account_key").Optional().Comment("ACME 账号私钥 PEM，首次注册后写入"),
		field.Text("acme_registration").Optional().Comment("ACME 注册信息 JSON，首次注册后写入"),

		field.String("scan_interval").NotEmpty().Default("24h").Comment("扫描间隔，Go duration 格式"),
		field.Int("renew_before_days").Default(30).Comment("证书到期前多少天续期"),

		field.String("jwt_secret").Optional().Comment("JWT 签名密钥，首次启动时自动生成"),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("更新时间"),
	}
}
