package agentbundle

import "context"

type BundleRef struct {
	Commit string `json:"commit"`
	Tag    string `json:"tag"`
	Digest string `json:"digest"`
}

type Store interface {
	CreateInitial(context.Context, string, string, string, string) (BundleRef, error)
	Verify(context.Context, string, string, BundleRef) error
	MaterializeVersion(context.Context, string, string, string, BundleRef) (string, error)
	MaterializeChat(context.Context, string, string, string, string, BundleRef) (string, error)
	MaterializeValidation(context.Context, string, string, string, BundleRef) (string, error)
}
