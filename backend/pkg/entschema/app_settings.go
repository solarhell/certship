package entschema

import (
	"fmt"
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

		field.Text("acme_account_key").Optional().Comment("ACME 账号私钥 PEM，首次注册后写入"),
		field.Text("acme_registration").Optional().Comment("ACME 注册信息 JSON，首次注册后写入"),

		field.String("scan_interval").
			NotEmpty().
			Default("24h").
			Validate(validDuration("scan_interval")).
			Comment("扫描间隔，Go duration 格式"),
		field.String("missing_grace").
			NotEmpty().
			Default("72h").
			Validate(validDuration("missing_grace")).
			Comment("域名连续多久未在云上扫到才判定为 missing,Go duration 格式"),

		field.String("archive_after").
			NotEmpty().
			Default("168h").
			Validate(validDuration("archive_after")).
			Comment("域名进入 missing 后再过多久归档并停止托管,Go duration 格式"),

		field.Int("renew_before_days").Min(1).Max(60).Default(30).Comment("证书到期前多少天续期"),

		field.String("archived_retention").
			NotEmpty().
			Default("2160h").
			Validate(validDuration("archived_retention")).
			Comment("归档且证书已过期的域名记录保留多久后物理删除,0s 表示永久保留"),

		field.String("dns_resolvers").
			NotEmpty().
			Default("223.5.5.5:53,119.29.29.29:53").
			Comment("做 zone 探测与 DNS-01 校验时使用的递归 DNS,逗号分隔的 host:port"),

		field.String("jwt_secret").Optional().Comment("JWT 签名密钥，首次启动时自动生成"),

		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("更新时间"),
	}
}

// validDuration 返回一个校验函数,确保字段值是合法的 Go duration。
func validDuration(fieldName string) func(string) error {
	return func(s string) error {
		if _, err := time.ParseDuration(s); err != nil {
			return fmt.Errorf("%s 不是合法的 duration: %w", fieldName, err)
		}
		return nil
	}
}
