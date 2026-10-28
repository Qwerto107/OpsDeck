package app

import (
	"context"
	"crypto/cipher"
	"errors"
)

type cloudRouter struct{ providers map[string]Cloud }

func NewCloud(key cipher.AEAD) Cloud {
	return &cloudRouter{providers: map[string]Cloud{"tencent": &Tencent{Key: key}, "alibaba": &Alibaba{Key: key}}}
}
func (r *cloudRouter) provider(ac CloudAccount) (Cloud, error) {
	p, ok := r.providers[ac.Provider]
	if !ok {
		return nil, errors.New("不支持的云厂商")
	}
	return p, nil
}
func (r *cloudRouter) Kinds(ac CloudAccount) []string {
	p, e := r.provider(ac)
	if e != nil {
		return nil
	}
	return p.Kinds(ac)
}
func (r *cloudRouter) Regions(ctx context.Context, ac CloudAccount, kind string) ([]string, error) {
	p, e := r.provider(ac)
	if e != nil {
		return nil, e
	}
	return p.Regions(ctx, ac, kind)
}
func (r *cloudRouter) List(ctx context.Context, ac CloudAccount, region, kind string) ([]Resource, error) {
	p, e := r.provider(ac)
	if e != nil {
		return nil, e
	}
	return p.List(ctx, ac, region, kind)
}
func (r *cloudRouter) Execute(ctx context.Context, ac CloudAccount, res Resource, action string) (string, error) {
	p, e := r.provider(ac)
	if e != nil {
		return "", e
	}
	return p.Execute(ctx, ac, res, action)
}
func (r *cloudRouter) State(ctx context.Context, ac CloudAccount, res Resource) (CloudState, error) {
	p, e := r.provider(ac)
	if e != nil {
		return CloudState{}, e
	}
	return p.State(ctx, ac, res)
}
func (r *cloudRouter) MonitorBatch(ctx context.Context, ac CloudAccount, rows []Resource, metrics []string) map[string]map[string]Metric {
	p, e := r.provider(ac)
	if e != nil {
		return map[string]map[string]Metric{}
	}
	return p.MonitorBatch(ctx, ac, rows, metrics)
}
