package access

import "context"

type bearerKey struct{}

func WithBearer(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, bearerKey{}, token)
}
func Bearer(ctx context.Context) string { value, _ := ctx.Value(bearerKey{}).(string); return value }
