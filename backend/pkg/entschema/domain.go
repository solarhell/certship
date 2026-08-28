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

type Domain struct {
	ent.Schema
}

func (Domain) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("t_domain"),
		entsql.WithComments(true),
	}
}

func (Domain) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Unique().Immutable().NotEmpty().DefaultFunc(uuid.NewString).Comment("主键 UUID"),

		field.String("domain").NotEmpty().Comment("自定义域名"),
		// bucket/region 对纯 CDN 域名(源站非 OSS)可能为空,只是源站信息而非域名身份
		field.String("bucket").Optional().Comment("OSS bucket 名称,CDN 域名时为回源 bucket,源站非 OSS 时为空"),
		field.String("region").Optional().Comment("OSS 区域,如 cn-hangzhou"),
		field.String("account_name").NotEmpty().Comment("阿里云账号名称"),

		field.Time("issued_at").Optional().Nillable().Comment("证书颁发时间"),
		field.Time("expires_at").Optional().Nillable().Comment("证书过期时间"),

		field.Text("cert_pem").Optional().Comment("证书 PEM 内容"),
		field.Text("key_pem").Optional().Comment("证书私钥 PEM 内容"),

		field.Enum("status").
			Values("pending", "active", "error").
			Default("pending").
			Comment("证书状态：pending=待颁发，active=有效，error=错误"),

		field.Enum("deploy_target").
			Values("oss", "cdn").
			Default("oss").
			Comment("部署目标：oss=直连 OSS，cdn=CDN 加速"),

		// ---- 存在性(云上是否还有这个域名),与证书状态正交 ----

		field.Enum("presence").
			Values("present", "missing", "archived").
			Default("present").
			Comment("云上存在性：present=在云上，missing=连续多轮未扫到，archived=已确认下线"),

		field.Enum("origin").
			Values("oss", "cdn", "both").
			Default("oss").
			Comment("发现来源：oss=OSS cname，cdn=CDN 加速域名，both=两侧都有"),

		field.Time("last_seen_at").
			Optional().
			Nillable().
			Comment("最后一次在云上扫描到的时间,用于判定下线"),

		field.Bool("managed").
			Default(true).
			Comment("是否由 certship 托管签发/续期,false=暂停托管(证书由别处管理)"),

		// ---- 失败退避 ----

		field.Int("retry_count").
			Default(0).
			Min(0).
			Comment("连续失败次数,成功后清零"),

		field.Time("next_retry_at").
			Optional().
			Nillable().
			Comment("下次允许重试的时间,未到不建续期任务"),

		field.Enum("error_kind").
			Values("none", "transient", "permanent", "rate_limited").
			Default("none").
			Comment("最近一次错误的分类：transient=可重试，permanent=需人工介入，rate_limited=被限速"),

		// ---- 告警去噪 ----

		field.String("notified_state").
			Optional().
			Comment("上次已通知的状态(ok/failed/blocked/missing/archived),用于只在状态变化时告警"),

		field.Time("last_notified_at").
			Optional().
			Nillable().
			Comment("上次发出告警的时间,用于持续失败时按递增间隔提醒"),

		field.Text("error_message").
			Optional().
			Comment("最近一次错误信息"),

		field.Text("blocked_reason").
			Optional().
			Default("").
			Comment("无法自动续期的阻塞原因(如 DNS 不在阿里云/未添加对应账号),非空表示跳过续期"),

		field.Time("created_at").Immutable().Default(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("创建时间"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).Annotations(entsql.Default("CURRENT_TIMESTAMP")).Comment("更新时间"),
	}
}

func (Domain) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("domain").Unique(),
		index.Fields("expires_at"),
		index.Fields("status"),
		index.Fields("presence"),
		index.Fields("last_seen_at"),
		index.Fields("next_retry_at"),
	}
}
