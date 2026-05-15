package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	entdialect "entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/config"
	"github.com/solarhell/certship/pkg/ent"
)

var (
	client *ent.Client
	once   sync.Once
)

func GetClient() *ent.Client {
	if client == nil {
		panic("database client 未初始化，请先调用 Setup")
	}
	return client
}

func Setup(cfg config.DatabaseConfig) {
	once.Do(func() {
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai",
			cfg.Host,
			cfg.Port,
			cfg.Username,
			cfg.Password,
			cfg.DB,
		)

		pgxCfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			panic(fmt.Errorf("解析 PostgreSQL DSN 失败: %w", err))
		}

		db := stdlib.OpenDB(*pgxCfg)
		db.SetMaxIdleConns(5)
		db.SetMaxOpenConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)

		if err := db.PingContext(context.Background()); err != nil {
			panic(fmt.Errorf("PostgreSQL ping 失败: %w", err))
		}

		drv := entsql.OpenDB(entdialect.Postgres, db)
		client = ent.NewClient(ent.Driver(drv))

		zap.L().Info("数据库连接成功，开始执行 migration")
		if err := client.Schema.Create(context.Background()); err != nil {
			panic(fmt.Errorf("数据库 migration 失败: %w", err))
		}
		zap.L().Info("数据库 migration 完成")
	})
}
