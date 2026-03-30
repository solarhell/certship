package ctxadmin

import (
	"context"

	"github.com/solarhell/certship/pkg/ent"
)

type ctxMarker struct{}

var ctxMarkerKey = &ctxMarker{}

type AuthContext struct {
	Token  string
	UserID string
	User   *ent.User
}

func SetInContext(ctx context.Context, info AuthContext) context.Context {
	return context.WithValue(ctx, ctxMarkerKey, info)
}

func MustExtract(ctx context.Context) AuthContext {
	info, ok := ctx.Value(ctxMarkerKey).(AuthContext)
	if !ok {
		panic("ctx does not contain AuthContext")
	}
	return info
}
