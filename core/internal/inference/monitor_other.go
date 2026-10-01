//go:build !linux && !windows

package inference

type platformSampler struct{}

func newPlatformSampler() platformSampler   { return platformSampler{} }
func (p *platformSampler) sample() Snapshot { return Snapshot{} }
