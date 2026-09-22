package identity

import "context"

type principalContextKey struct{}

// WithPrincipal binds the trusted installation principal to an operation.
// HTTP middleware is the only place that derives this value from a request;
// JSON bodies never control it.
func WithPrincipal(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, principalContextKey{}, id)
}

func Principal(ctx context.Context) string {
	value, _ := ctx.Value(principalContextKey{}).(string)
	return value
}
