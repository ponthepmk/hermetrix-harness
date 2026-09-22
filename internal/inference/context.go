package inference

import "context"

type ownerContextKey struct{}

type Owner struct {
	Kind                     string
	ID                       string
	SessionID                string
	TurnID                   string
	Source                   string
	Priority                 Priority
	TokenLimit               int
	PresetID                 string
	PresetRevision           int
	PresetRole               string
	RuntimeFingerprintID     string
	EffectiveParameterDigest string
}

func WithOwner(ctx context.Context, owner Owner) context.Context {
	return context.WithValue(ctx, ownerContextKey{}, owner)
}

func OwnerFromContext(ctx context.Context) Owner {
	owner, _ := ctx.Value(ownerContextKey{}).(Owner)
	return owner
}
